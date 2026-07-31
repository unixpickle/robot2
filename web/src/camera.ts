import './style/camera.css';

interface APIResponse<T> {
  error?: string;
  data?: T;
}

interface ConnectResponse {
  session: string;
  offer: RTCSessionDescriptionInit;
}

interface ICECandidatesResponse {
  candidates: [RTCIceCandidateInit];
  done: boolean;
}

type OnStream = (stream: MediaStream) => void;
type OnClose = () => void;
type OnError = (error: string) => void;

export class CameraView {
  public element: HTMLElement;

  private conn?: CameraRTCConnection;
  private track: string;

  constructor(track: string) {
    this.track = track;
    this.element = document.createElement('div');
    this.element.className = 'camera';
    this.connect();

    window.addEventListener('pagehide', () => {
      this.disconnect();
    });
    window.addEventListener('visibilitychange', () => {
      if (document.hidden) {
        this.disconnect();
      } else {
        if (!this.conn?.isOpen()) {
          this.connect();
        }
      }
    });
  }

  private connect() {
    if (this.conn) {
      this.conn.close();
    }
    this.showLoading();
    this.conn = new CameraRTCConnection(this.track);
    this.conn.onclose = () => this.showError('connection closed');
    this.conn.onerror = (e) => this.showError(e);
    this.conn.onstream = (stream) => this.showStream(stream);
    this.conn.connect();
  }

  private disconnect() {
    this.conn?.close();
    this.showError('Disconnected by user event.');
  }

  private showLoading() {
    this.element.innerHTML = '<div class="camera-loader"></div>';
  }

  private removeLoader() {
    const loaders = this.element.getElementsByClassName('camera-loader');
    for (let i = 0; i < loaders.length; i++) {
      this.element.removeChild(loaders[i]);
    }
  }

  private showError(err: string) {
    this.element.innerHTML = '';
    const errElement = document.createElement('div');
    errElement.className = 'camera-error';

    const errLabel = document.createElement('label');
    errLabel.className = 'camera-error-message';
    errLabel.textContent = err;
    errElement.appendChild(errLabel);

    const errRetry = document.createElement('button');
    errRetry.className = 'camera-error-retry';
    errRetry.textContent = 'Retry';
    errRetry.addEventListener('click', () => this.connect());
    errElement.appendChild(errRetry);

    this.element.appendChild(errElement);
  }

  private showStream(stream: MediaStream) {
    this.showLoading();
    const vidElement = document.createElement('video');
    vidElement.autoplay = true;
    vidElement.playsInline = true; // maybe helps for mobile browsers
    vidElement.muted = true; // without this, chrome refuses to play before user interaction
    vidElement.srcObject = stream;
    vidElement.className = 'camera-video';
    vidElement.addEventListener(
      'loadedmetadata',
      () => {
        this.removeLoader();
        vidElement.play();
      },
      {
        once: true,
      },
    );
    this.element.appendChild(vidElement);
  }
}

class CameraRTCConnection {
  private track: string;
  private pc: RTCPeerConnection;
  private session?: string;
  private closed: boolean = false;

  public onclose: OnClose = () => null;
  public onerror: OnError = (_) => null;
  public onstream: OnStream = (_) => null;

  constructor(track: string) {
    this.track = track;
    this.pc = new RTCPeerConnection({
      iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
    });
    this.closed = false;
    this.pc.addEventListener('connectionstatechange', (event) => {
      if (
        this.pc.connectionState == 'closed' ||
        this.pc.connectionState == 'failed'
      ) {
        if (!this.closed) {
          this.closed = true;
          this.pc.close();
          if (this.pc.connectionState == 'closed') {
            this.onclose();
          } else {
            this.onerror('RTC connection in failed state');
          }
        }
      }
    });
    this.pc.addEventListener('track', (event) => {
      this.onstream(event.streams[0]);
    });
  }

  public isOpen(): boolean {
    return !this.closed;
  }

  public close() {
    if (!this.closed) {
      this.closed = true;
      this.pc.close();

      if (this.session) {
        navigator.sendBeacon(
          `/camera/disconnect?session=${encodeURIComponent(this.session)}`,
        );
      }
    }
  }

  public async connect() {
    try {
      const created: ConnectResponse = await this.apiRequest(
        `connect?track=${encodeURIComponent(this.track)}`,
        { track: this.track },
      );
      this.session = created.session;
      this.pc.addEventListener('icecandidate', (event) => {
        const candidates = event.candidate ? [event.candidate] : [];
        this.apiRequest<any>('addicecandidates', candidates).catch((e) => {
          this.flagError(e);
        });
      });
      await this.pc.setRemoteDescription(created.offer);
      const answer = await this.pc.createAnswer();
      await this.pc.setLocalDescription(answer);
      await this.apiRequest<any>('answer', this.pc.localDescription);
      await this.pollICE();
    } catch (e) {
      this.flagError(e);
    }
  }

  private flagError(err: any) {
    if (!this.closed) {
      this.pc.close();
      this.closed = true;
      this.onerror('error establishing RTC connection: ' + err);
    }
  }

  private async pollICE() {
    let seen: number = 0;
    while (true) {
      if (this.closed) {
        return;
      }
      const nextResponse: ICECandidatesResponse = await this.apiRequest(
        'icecandidates',
        {},
      );
      for (let i = seen; i < nextResponse.candidates.length; i++) {
        await this.pc.addIceCandidate(nextResponse.candidates[i]);
      }
      seen = nextResponse.candidates.length;
      if (nextResponse.done) {
        this.pc.addIceCandidate();
        return;
      }

      // Wait before next poll.
      await wait(2000);
    }
  }

  private async apiRequest<T>(apiName: string, payload: any): Promise<T> {
    try {
      let url = '/camera/' + apiName;
      if (this.session) {
        url = url + '?session=' + this.session;
      }
      const result = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      const obj: APIResponse<T> = await result.json();
      if (obj['error']) {
        throw 'error from server:' + obj.error;
      }
      return obj.data as T;
    } catch (e) {
      throw 'camera api ' + apiName + ' failed with error: ' + e;
    }
  }
}

async function wait(t: number): Promise<any> {
  return new Promise((resolve, reject) => {
    setTimeout(function () {
      resolve(null);
    }, t);
  });
}
