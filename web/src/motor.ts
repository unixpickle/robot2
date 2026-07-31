import './style/motor.css';

const MotorNames: string[] = [
  'shoulder_pan',
  'shoulder_lift',
  'elbow_flex',
  'wrist_flex',
  'wrist_roll',
  'gripper',
];

interface PositionLimit {
  min: number;
  max: number;
}

interface MotorStatus {
  id: number;
  errorFlags: number;
  position: number;
  speed: number;
  load: number;
  temperature: number;
  asyncFlag: number;
  status: number;
  moving: number;
  voltage: number;
  current: number;
  relativePos: number;
  positionLimit: PositionLimit;
  targetPos: number;
  targetRelativePos: number;
}

interface MotorStatuses {
  [key: string]: MotorStatus;
}

interface APIResponse<T> {
  error?: string;
  data?: T;
}

type OnStatus = (statuses: MotorStatuses) => void;
type OnDisconnect = (error: any) => void;
type OnError = (error: any) => void;
type OnChangeTarget = (target: number) => Promise<any>;

export class MotorController {
  public element: HTMLElement;
  private error: HTMLElement;

  constructor() {
    this.element = document.createElement('div');
    this.element.className = 'motor-container';

    this.error = document.createElement('div');
    this.error.classList.add('motors-error');
    this.error.classList.add('motors-error-hidden');

    const client = new MotorClient();
    const singleViews = MotorNames.map((name) => {
      const view = new SingleMotorView(name);
      this.element.appendChild(view.element);
      view.onChangeTarget = (target) => client.moveMotor(name, target);
      return view;
    });

    client.onStatus = (statuses: MotorStatuses) => {
      MotorNames.forEach((name, idx) => {
        singleViews[idx].handleStatus(statuses[name]);
      });
    };
  }

  showError(err: string | null) {
    if (err == null) {
      this.error.classList.add('motors-error-hidden');
    } else {
      this.error.classList.remove('motors-error-hidden');
      this.error.textContent = err;
    }
  }
}

class SingleMotorView {
  public element: HTMLElement;
  public onChangeTarget: OnChangeTarget = async (_) => null;
  private slider: HTMLInputElement;
  private currentContainer: HTMLElement;
  private currentValue: HTMLElement;
  private lastUserChange: number = -Infinity;

  private moveRequestInFlight = false;

  constructor(name: string) {
    this.element = document.createElement('div');
    this.element.className = 'motor-single';

    const nameLabel = document.createElement('label');
    nameLabel.className = 'motor-name';
    nameLabel.textContent = name;
    this.element.appendChild(nameLabel);

    this.slider = document.createElement('input');
    this.slider.type = 'range';
    this.slider.min = '0';
    this.slider.max = '1';
    this.slider.step = '0.01';
    this.slider.className = 'motor-slider';
    this.slider.addEventListener('input', async () => {
      this.lastUserChange = performance.now();
      if (this.moveRequestInFlight) {
        // Allow the previous request to finish first to avoid
        // spamming the server with out-of-order targets.
        return;
      }
      this.moveRequestInFlight = true;
      let prevValue = this.slider.valueAsNumber;
      while (true) {
        await this.onChangeTarget(prevValue);
        const newValue = this.slider.valueAsNumber;
        if (newValue == prevValue) {
          break;
        }
        prevValue = newValue;
      }
      this.moveRequestInFlight = false;
    });
    this.element.appendChild(this.slider);

    this.currentContainer = document.createElement('div');
    this.currentContainer.className = 'motor-position-pointer-container';
    this.currentValue = document.createElement('div');
    this.currentValue.className = 'motor-position-pointer';
    this.currentContainer.appendChild(this.currentValue);
    this.element.appendChild(this.currentContainer);
  }

  handleStatus(status: MotorStatus) {
    this.currentValue.style.left = (status.relativePos * 100).toFixed(4) + '%';

    // Only update the slider if the user hasn't touched it recently.
    if (performance.now() - this.lastUserChange > 5000) {
      this.slider.value = '' + status.targetRelativePos;
    }
  }
}

class MotorClient {
  public onStatus: OnStatus = (_) => null;
  public onDisconnect: OnDisconnect = (_) => null;
  public onError: OnError = (_) => null;

  constructor() {
    const events = new EventSource('/motor/stream');
    events.onmessage = (event) => {
      this.onStatus(JSON.parse(event.data));
    };
    events.onerror = (event) => {
      if (events.readyState === EventSource.CONNECTING) {
        this.onDisconnect(event);
      } else {
        this.onError('Motor event stream has failed.');
      }
    };
  }

  async moveMotor(motor: string, relPos: number) {
    await apiRequest(
      `move?motor=${encodeURIComponent(motor)}&rel=${encodeURIComponent(relPos + '')}`,
    );
  }
}

async function apiRequest<T>(path: string, payload?: any): Promise<T> {
  try {
    const url = '/motor/' + path;
    const result = await fetch(
      url,
      payload
        ? {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
          }
        : {},
    );
    const obj: APIResponse<T> = await result.json();
    if (obj['error']) {
      throw 'error from server:' + obj.error;
    }
    return obj.data as T;
  } catch (e) {
    throw 'motor ' + path + ' failed with error: ' + e;
  }
}
