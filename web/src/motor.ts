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

    const controls = document.createElement('div');
    controls.className = 'motors-controls';
    this.element.appendChild(controls);

    const home = document.createElement('button');
    home.className = 'motors-home-button';
    home.textContent = 'Home';
    home.addEventListener('click', async () => {
      const motors = await apiRequest<[string]>('names');
      for (const motor of motors) {
        await apiRequest(`move?motor=${motor}&pos=2048`);
      }
    });
    controls.appendChild(home);

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
  public currentLabel: HTMLLabelElement;
  public loadLabel: HTMLLabelElement;
  public onChangeTarget: OnChangeTarget = async (_) => null;
  private targetSlider: Slider;
  private stateSlider: Slider;
  private lastUserChange: number = -Infinity;

  // Used to prevent overloading the server with small changes
  private moveRequestInFlight = false;

  constructor(name: string) {
    this.element = document.createElement('div');
    this.element.className = 'motor-single';

    const infoContainer = document.createElement('div');
    infoContainer.className = 'motor-info';
    this.element.appendChild(infoContainer);

    const nameLabel = document.createElement('label');
    nameLabel.className = 'motor-label-name';
    nameLabel.textContent = name;
    infoContainer.appendChild(nameLabel);

    this.currentLabel = document.createElement('label');
    this.currentLabel.className = 'motor-label-current';
    this.currentLabel.textContent = '-';
    infoContainer.appendChild(this.currentLabel);

    this.loadLabel = document.createElement('label');
    this.loadLabel.className = 'motor-label-load';
    this.loadLabel.textContent = '-';
    infoContainer.appendChild(this.loadLabel);

    this.targetSlider = new Slider('motor-target-slider');
    this.element.appendChild(this.targetSlider.element);
    this.targetSlider.slider.addEventListener('input', async () => {
      this.lastUserChange = performance.now();
      if (this.moveRequestInFlight) {
        // Allow the previous request to finish first to avoid
        // spamming the server with out-of-order targets.
        return;
      }
      this.moveRequestInFlight = true;
      let prevValue = this.targetSlider.value();
      while (true) {
        await this.onChangeTarget(prevValue);
        const newValue = this.targetSlider.value();
        if (newValue == prevValue) {
          break;
        }
        prevValue = newValue;
      }
      this.moveRequestInFlight = false;
    });
    this.stateSlider = new Slider('motor-state-slider');
    this.element.appendChild(this.stateSlider.element);
  }

  handleStatus(status: MotorStatus) {
    this.stateSlider.setValue(status.relativePos);
    this.currentLabel.textContent = status.current.toFixed(1) + 'mA';
    this.loadLabel.textContent = status.load + '';

    // Only update the slider if the user hasn't touched it recently.
    if (performance.now() - this.lastUserChange > 5000) {
      this.targetSlider.setValue(status.targetRelativePos);
    }
  }
}

class Slider {
  public element: HTMLElement;
  public slider: HTMLInputElement;
  private label: HTMLLabelElement;

  constructor(clsName: string) {
    this.slider = document.createElement('input');
    this.slider.type = 'range';
    this.slider.min = '0';
    this.slider.max = '1';
    this.slider.step = '0.001';
    this.slider.className = clsName;

    this.element = document.createElement('div');
    this.element.className = 'motor-slider-container';
    this.element.appendChild(this.slider);

    this.label = document.createElement('label');
    this.label.className = 'motor-slider-label';
    this.element.appendChild(this.label);

    this.slider.addEventListener('input', () => this.updateLabel());
    this.updateLabel();
  }

  private updateLabel() {
    this.label.textContent = this.slider.valueAsNumber.toFixed(2);
  }

  public value(): number {
    return this.slider.valueAsNumber;
  }

  public setValue(value: number) {
    this.slider.value = '' + value;
    this.updateLabel();
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
