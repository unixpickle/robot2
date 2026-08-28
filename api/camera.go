package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

func (c *Client) CameraTrackNames() ([]string, error) {
	var result []string
	if err := c.doRequest("/camera/tracknames", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Snapshot(trackName string) ([]byte, error) {
	var obj struct {
		Track string `json:"track"`
	}
	obj.Track = trackName
	resp, err := c.responseForRequest("/camera/snapshot", "", obj)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var fullOut struct {
			Error *string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&fullOut); err != nil {
			return nil, err
		}
		if fullOut.Error != nil {
			return nil, &RemoteError{Message: *fullOut.Error}
		}
		return nil, fmt.Errorf("unexpected status code from camera snapshot: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) StreamCamera(
	ctx context.Context,
	trackName string,
	bufSize int,
) (<-chan media.Sample, <-chan error) {
	innerCtx, cancel := context.WithCancel(ctx)
	result := &rtcStream{
		client:     c,
		innerCtx:   innerCtx,
		cancel:     cancel,
		sampleChan: make(chan media.Sample, bufSize),
		errChan:    make(chan error, 1),
	}
	go result.Connect(trackName)
	return result.sampleChan, result.errChan
}

func (c *Client) RecordCamera(path, trackName string, fps int) (*CameraRecorder, error) {
	cmd := exec.Command(
		"ffmpeg",
		// Raw Annex-B H.264 on stdin.
		"-r", fmt.Sprintf("%d", fps),
		"-f", "h264",
		"-i", "pipe:0",
		"-an",
		"-c:v", "copy",
		"-movflags", "+faststart",
		"-y",
		path,
	)

	inPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard

	ctx, cancel := context.WithCancelCause(context.Background())
	sampleCh, errCh := c.StreamCamera(ctx, trackName, 24)

	go func() {
		defer inPipe.Close()
		for sample := range sampleCh {
			if _, err := inPipe.Write(sample.Data); err != nil {
				cancel(err)
				return
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		cancel(err)
		return nil, err
	}

	return &CameraRecorder{
		errChan: errCh,
		ctx:     ctx,
		cancel:  cancel,
		proc:    cmd,
	}, nil
}

type rtcStream struct {
	client *Client

	innerCtx context.Context
	cancel   func()

	closedLock sync.RWMutex
	closed     bool
	sampleChan chan media.Sample
	errChan    chan error

	sessionID string

	pc *webrtc.PeerConnection
}

func (r *rtcStream) Connect(trackName string) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	if err != nil {
		r.endWithError(err)
		return
	}
	r.pc = pc
	go func() {
		<-r.innerCtx.Done()

		// This will only do something if the actual cause of innerCtx being done
		// was the user-provided context being marked done.
		// Otherwise, the innerCtx was already canceled by another error.
		//
		// We do this before pc.Close() to avoid a race condition where a canceled
		// context's error isn't reported because pc.Close() causes another error
		// elsewhere first.
		r.endWithError(r.innerCtx.Err())

		pc.Close()
	}()

	var connectResponse struct {
		Session string                    `json:"session"`
		Offer   webrtc.SessionDescription `json:"offer"`
	}
	params := map[string]string{"track": trackName}
	if err := r.client.doRequest("/camera/connect", params, &connectResponse); err != nil {
		r.endWithError(err)
		return
	}

	r.sessionID = connectResponse.Session
	go func() {
		// Fast remote cleanup path when we are closed.
		<-r.innerCtx.Done()
		r.sessionRequest("/camera/disconnect", nil, nil)
	}()

	pc.OnICECandidate(r.handleICECandidate)
	pc.OnTrack(r.handleTrack)
	pc.OnConnectionStateChange(func(pcs webrtc.PeerConnectionState) {
		if pcs == webrtc.PeerConnectionStateClosed || pcs == webrtc.PeerConnectionStateFailed {
			r.endWithError(errors.New("connection is closed or failed"))
		}
	})
	pc.SetRemoteDescription(connectResponse.Offer)
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		r.endWithError(err)
		return
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		r.endWithError(err)
		return
	}
	if err := r.sessionRequest("/camera/answer", answer, nil); err != nil {
		r.endWithError(err)
		return
	}

	go r.pollICECandidates()
}

func (r *rtcStream) handleICECandidate(candidate *webrtc.ICECandidate) {
	cs := []*webrtc.ICECandidate{}
	if candidate != nil {
		cs = append(cs, candidate)
	}
	if err := r.sessionRequest("/camera/addicecandidates", cs, nil); err != nil {
		r.endWithError(err)
	}
}

func (r *rtcStream) handleTrack(track *webrtc.TrackRemote, recv *webrtc.RTPReceiver) {
	builder := samplebuilder.New(
		128,
		&codecs.H264Packet{},
		90000,
	)
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			r.endWithError(fmt.Errorf("read RTP: %w", err))
			return
		}
		builder.Push(packet)
		for {
			sample := builder.Pop()
			if sample == nil {
				break
			}
			r.sendSample(*sample)
		}
	}
}

func (r *rtcStream) pollICECandidates() {
	for !r.Closed() {
		var candidatesResp struct {
			Candidates []webrtc.ICECandidateInit `json:"candidates"`
			Done       bool                      `json:"done"`
		}
		if err := r.sessionRequest("/camera/icecandidates", nil, &candidatesResp); err != nil {
			r.endWithError(err)
			return
		}
		for _, c := range candidatesResp.Candidates {
			if err := r.pc.AddICECandidate(c); err != nil {
				r.endWithError(err)
				return
			}
		}
		if candidatesResp.Done {
			return
		}
		select {
		case <-r.innerCtx.Done():
		case <-time.After(time.Second):
		}
	}
}

func (r *rtcStream) sessionRequest(path string, objIn, objOut any) error {
	if err := r.client.doRequestWithQuery(path, "session="+r.sessionID, objIn, objOut); err != nil {
		return fmt.Errorf("RTC session request to %s: %w", path, err)
	}
	return nil
}

func (r *rtcStream) endWithError(err error) {
	r.closedLock.Lock()
	defer r.closedLock.Unlock()
	if r.closed {
		return
	}
	r.errChan <- err
	r.closed = true
	close(r.errChan)
	close(r.sampleChan)
	r.cancel()
}

func (r *rtcStream) sendSample(sample media.Sample) {
	r.closedLock.RLock()
	defer r.closedLock.RUnlock()
	if r.closed {
		return
	}
	select {
	case r.sampleChan <- sample:
	default:
	}
}

func (r *rtcStream) Closed() bool {
	r.closedLock.RLock()
	defer r.closedLock.RUnlock()
	return r.closed
}

var errCameraRecorderStopped = errors.New("camera recorder stopped")

// A CameraRecorder controls an active process that is streaming a camera into
// a video file.
type CameraRecorder struct {
	errChan <-chan error
	ctx     context.Context
	cancel  func(error)
	proc    *exec.Cmd
}

// Stop gracefully stops the recording and waits for completion.
func (c *CameraRecorder) Stop() error {
	c.cancel(errCameraRecorderStopped)
	procErr := c.proc.Wait()
	err := <-c.errChan
	if err == nil {
		panic("camera stream error channel must return non-nil error")
	}
	if !(errors.Is(err, context.Canceled) && errors.Is(context.Cause(c.ctx), errCameraRecorderStopped)) {
		return err
	}
	if procErr != nil {
		return fmt.Errorf("ffmpeg returned error: %w", procErr)
	}
	return nil
}

// Cancel ungracefully terminates the recording and waits for the process to be
// cleaned up.
func (c *CameraRecorder) Cancel() {
	c.cancel(nil)
	c.proc.Process.Kill()
	c.proc.Wait()
}
