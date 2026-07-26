package camera

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

type WebError struct {
	Code    int
	Message string
}

func (w *WebError) Error() string {
	return w.Message
}

var (
	errNoSession       = &WebError{Code: http.StatusBadRequest, Message: "no RTC session found"}
	errMissingTrack    = &WebError{Code: http.StatusBadRequest, Message: "no track specified"}
	errNoTrackFound    = &WebError{Code: http.StatusBadRequest, Message: "unknown track"}
	errAlreadyAnswered = &WebError{Code: http.StatusBadRequest, Message: "answer already received for this session"}
)

type WebRTCSessions struct {
	sessionTimeout time.Duration
	tracks         []*CameraTrack

	// maps string to *webRTCSession
	sessions *sync.Map

	mux *http.ServeMux
}

func NewWebRTCSessions(tracks []*CameraTrack, sessionTimeout time.Duration) *WebRTCSessions {
	mux := http.NewServeMux()

	w := &WebRTCSessions{
		sessionTimeout: sessionTimeout,
		tracks:         tracks,

		sessions: &sync.Map{},
		mux:      mux,
	}

	mux.HandleFunc("/connect", w.handleConnect)
	mux.HandleFunc("/disconnect", w.handleDisconnect)
	mux.HandleFunc("/icecandidates", w.handleICECandidates)
	mux.HandleFunc("/addicecandidates", w.handleRemoteICECandidates)
	mux.HandleFunc("/answer", w.handleAnswer)
	mux.HandleFunc("/status", w.handleStatus)

	go w.cleanupWorker()
	return w
}

func (w *WebRTCSessions) ServeHTTP(wr http.ResponseWriter, r *http.Request) {
	w.mux.ServeHTTP(wr, r)
}

func (w *WebRTCSessions) handleConnect(wr http.ResponseWriter, r *http.Request) {
	var payload struct {
		Track string `json:"track"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.serveError(wr, &WebError{Code: http.StatusBadRequest, Message: err.Error()})
		return
	}
	if payload.Track == "" {
		w.serveError(wr, errMissingTrack)
		return
	}
	var track *CameraTrack
	for _, t := range w.tracks {
		if t.Name == payload.Track {
			track = t
			break
		}
	}
	if track == nil {
		w.serveError(wr, errNoTrackFound)
		return
	}
	if err := track.LastError(); err != nil {
		w.serveError(wr, err)
		return
	}

	session, err := newWebRTCSession(track)
	if err != nil {
		w.serveError(wr, err)
		return
	}

	w.sessions.Store(session.ID, session)
	w.serveData(wr, map[string]any{
		"session": session.ID,
		"offer":   session.LocalDescription(),
	})
}

func (w *WebRTCSessions) handleDisconnect(wr http.ResponseWriter, r *http.Request) {
	w.handleSessionAPI(wr, r, func(s *webRTCSession) (any, error) {
		s.Close()
		w.sessions.Delete(s.ID)
		return true, nil
	})
}

func (w *WebRTCSessions) handleICECandidates(wr http.ResponseWriter, r *http.Request) {
	w.handleSessionAPI(wr, r, func(s *webRTCSession) (any, error) {
		cands, done := s.ICECandidates()
		return map[string]any{"candidates": cands, "done": done}, nil
	})
}

func (w *WebRTCSessions) handleAnswer(wr http.ResponseWriter, r *http.Request) {
	w.handleSessionAPI(wr, r, func(s *webRTCSession) (any, error) {
		var answer webrtc.SessionDescription
		if err := json.NewDecoder(r.Body).Decode(&answer); err != nil {
			return nil, err
		}
		if err := s.HandleAnswer(answer); err != nil {
			return nil, err
		} else {
			return true, nil
		}
	})
}

func (w *WebRTCSessions) handleStatus(wr http.ResponseWriter, r *http.Request) {
	obj := map[string]any{}
	for _, t := range w.tracks {
		var cameraInfo struct {
			Metrics   *CameraTrackMetrics `json:"metrics"`
			LastError *string             `json:"lastError"`
		}
		cameraInfo.Metrics = t.Metrics()
		if err := t.LastError(); err != nil {
			errMsg := new(string)
			*errMsg = err.Error()
			cameraInfo.LastError = errMsg
		}
		obj[t.Name] = cameraInfo
	}
	w.serveData(wr, obj)
}

func (w *WebRTCSessions) handleRemoteICECandidates(wr http.ResponseWriter, r *http.Request) {
	w.handleSessionAPI(wr, r, func(s *webRTCSession) (any, error) {
		var candidates []webrtc.ICECandidateInit
		if err := json.NewDecoder(r.Body).Decode(&candidates); err != nil {
			return nil, err
		}
		return true, s.HandleICECandidates(candidates)
	})
}

func (w *WebRTCSessions) handleSessionAPI(
	wr http.ResponseWriter,
	r *http.Request,
	f func(s *webRTCSession) (any, error),
) {
	id := r.FormValue("session")
	sess, ok := w.sessions.Load(id)
	if !ok {
		w.serveError(wr, errNoSession)
		return
	}
	s := sess.(*webRTCSession)
	s.Keepalive()
	obj, err := f(s)
	if err != nil {
		w.serveError(wr, err)
	} else {
		w.serveData(wr, obj)
	}
}

func (w *WebRTCSessions) serveError(wr http.ResponseWriter, err error) {
	wr.Header().Set("content-type", "application/json")
	if err, ok := errors.AsType[*WebError](err); ok {
		wr.WriteHeader(err.Code)
	} else {
		wr.WriteHeader(http.StatusInternalServerError)
	}
	encoded, _ := json.Marshal(map[string]string{"error": err.Error()})
	wr.Write(encoded)
}

func (w *WebRTCSessions) serveData(wr http.ResponseWriter, obj any) {
	wr.Header().Set("content-type", "application/json")
	encoded, _ := json.Marshal(map[string]any{"data": obj})
	wr.Write(encoded)
}

func (w *WebRTCSessions) cleanupWorker() {
	for {
		w.deleteEndedSessions()
		time.Sleep(time.Second * 10)
	}
}

func (w *WebRTCSessions) deleteEndedSessions() {
	for k, v := range w.sessions.Range {
		v := v.(*webRTCSession)
		if v.ShouldDelete(w.sessionTimeout) {
			w.sessions.Delete(k)
			v.Close()
		}
	}
}

type webRTCSession struct {
	ID string

	Context context.Context
	cancel  func()

	Track *CameraTrack

	// contains time.Time
	lastKeepalive atomic.Value

	peerConn *webrtc.PeerConnection
	sender   *webrtc.RTPSender

	iceStateLock  sync.RWMutex
	iceCandidates []webrtc.ICECandidateInit
	iceDone       bool

	clientLock sync.Mutex

	pendingRemoteCandidates []webrtc.ICECandidateInit
	remoteCandidatesDone    bool
}

func newWebRTCSession(track *CameraTrack) (*webRTCSession, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// This context is used to trigger shutdown, maybe more than once,
	// in which case it's idempotent.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-ctx.Done()
		pc.Close()
	}()

	res := &webRTCSession{
		ID:       id.String(),
		Context:  ctx,
		cancel:   cancel,
		Track:    track,
		peerConn: pc,
	}
	res.Keepalive()

	res.handleCloseEvents()
	if err := res.setupSender(); err != nil {
		res.Close()
		return nil, err
	}

	if err := res.setupICEAndOffer(); err != nil {
		res.Close()
		return nil, err
	}

	return res, nil
}

func (w *webRTCSession) Close() {
	w.cancel()
}

func (w *webRTCSession) Keepalive() {
	w.lastKeepalive.Store(time.Now())
}

func (w *webRTCSession) ShouldDelete(timeout time.Duration) bool {
	select {
	case <-w.Context.Done():
		// This might happen if the RTC connection fails, and we call Close() but the
		// owning WebRTCSessions hasn't deleted us yet.
		return true
	default:
	}

	state := w.peerConn.ConnectionState()
	if state == webrtc.PeerConnectionStateConnected || state == webrtc.PeerConnectionStateDisconnected {
		// There won't be any keepalives during a live connection or during recovery.
		return false
	}

	// Keepalives are mostly used during initialization, where the client
	// makes a sequence of HTTP requests and may disappear at any time.
	return time.Since(w.lastKeepalive.Load().(time.Time)) > timeout
}

func (w *webRTCSession) LocalDescription() webrtc.SessionDescription {
	return *w.peerConn.LocalDescription()
}

func (w *webRTCSession) ICECandidates() ([]webrtc.ICECandidateInit, bool) {
	w.iceStateLock.RLock()
	defer w.iceStateLock.RUnlock()
	return append([]webrtc.ICECandidateInit{}, w.iceCandidates...), w.iceDone
}

func (w *webRTCSession) HandleAnswer(answer webrtc.SessionDescription) error {
	w.clientLock.Lock()
	defer w.clientLock.Unlock()
	if w.peerConn.RemoteDescription() != nil {
		return errAlreadyAnswered
	}
	if err := w.peerConn.SetRemoteDescription(answer); err != nil {
		return err
	}
	for _, x := range w.pendingRemoteCandidates {
		if err := w.peerConn.AddICECandidate(x); err != nil {
			return err
		}
	}
	w.pendingRemoteCandidates = nil
	return nil
}

func (w *webRTCSession) HandleICECandidates(candidates []webrtc.ICECandidateInit) error {
	w.clientLock.Lock()
	defer w.clientLock.Unlock()
	if w.peerConn.RemoteDescription() == nil {
		// We cannot add ICE candidates until we have an answer, which may already
		// be inflight but not here yet due to racing.
		w.pendingRemoteCandidates = append(w.pendingRemoteCandidates, candidates...)
		if len(candidates) == 0 {
			w.pendingRemoteCandidates = append(w.pendingRemoteCandidates, webrtc.ICECandidateInit{})
		}
		return nil
	}
	for _, x := range candidates {
		if err := w.peerConn.AddICECandidate(x); err != nil {
			return err
		}
	}
	if len(candidates) == 0 {
		// Signal end of candidates
		return w.peerConn.AddICECandidate(webrtc.ICECandidateInit{})
	}
	return nil
}

func (w *webRTCSession) handleCloseEvents() {
	w.peerConn.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed:
			w.Close()
		}
	})
}

func (w *webRTCSession) setupSender() error {
	sender, err := w.peerConn.AddTrack(w.Track.Track)
	if err != nil {
		return err
	}

	// Make sure we drain the sender's RTCP stream
	go func() {
		for {
			_, _, err := sender.ReadRTCP()
			if err != nil {
				return
			}
		}
	}()

	w.sender = sender
	return nil
}

func (w *webRTCSession) setupICEAndOffer() error {
	w.peerConn.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		w.iceStateLock.Lock()
		if candidate == nil {
			w.iceDone = true
		} else {
			w.iceCandidates = append(w.iceCandidates, candidate.ToJSON())
		}
		w.iceStateLock.Unlock()
	})

	offer, err := w.peerConn.CreateOffer(nil)
	if err != nil {
		return err
	}

	if err := w.peerConn.SetLocalDescription(offer); err != nil {
		return err
	}

	return nil
}
