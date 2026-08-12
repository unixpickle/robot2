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
  private loader: HTMLElement;
  private controls: HTMLElement;
  private globalControls: HTMLElement;
  private error: HTMLElement;

  constructor() {
    this.element = document.getElementsByClassName(
      'motor-container',
    )[0] as HTMLElement;

    this.error = document.createElement('div');
    this.error.classList.add('motors-error');

    this.loader = document.createElement('div');
    this.loader.classList.add('motors-loader');

    this.controls = document.createElement('div');

    const client = new MotorClient();
    const singleViews = MotorNames.map((name) => {
      const view = new SingleMotorView(name);
      view.onChangeTarget = (target) => client.moveMotor(name, target);
      return view;
    });
    for (const view of singleViews) {
      this.controls.appendChild(view.element);
    }

    this.globalControls = document.createElement('div');
    this.globalControls.className = 'motors-global-controls';

    const home = document.createElement('button');
    home.className = 'motors-home-button';
    home.textContent = 'Home';
    home.addEventListener('click', async () => {
      const motors = await apiRequest<[string]>('names');
      for (const motor of motors) {
        await apiRequest(`move?motor=${motor}&pos=2048`);
      }
    });
    this.globalControls.appendChild(home);
    this.controls.appendChild(this.globalControls);

    client.onStatus = (statuses: MotorStatuses) => {
      this.showControls();
      MotorNames.forEach((name, idx) => {
        singleViews[idx].handleStatus(statuses[name]);
      });
    };
    client.onDisconnect = (error: any) => this.showError('' + error);
    client.onError = (error: any) => this.showError('' + error);

    this.showLoader();
  }

  private showError(err: string) {
    this.showElement(this.error);
    this.error.textContent = err;
  }

  private showLoader() {
    this.showElement(this.loader);
  }

  private showControls() {
    this.showElement(this.controls);
  }

  private showElement(e: HTMLElement) {
    for (const el of [this.loader, this.error, this.controls]) {
      if (el == e) {
        if (!el.parentNode) {
          this.element.appendChild(el);
        }
      } else {
        if (el.parentNode) {
          this.element.removeChild(el);
        }
      }
    }
  }
}

class SingleMotorView {
  public name: string;
  public element: HTMLElement;
  public currentLabel: HTMLLabelElement;
  public loadLabel: HTMLLabelElement;
  public onChangeTarget: OnChangeTarget = async (_) => null;
  private torqueCheck: HTMLInputElement;
  private targetSlider: Slider;
  private stateSlider: Slider;
  private lastUserChange: number = -Infinity;

  // Used to prevent overloading the server with small changes
  private moveRequestInFlight = false;

  constructor(name: string) {
    this.name = name;

    this.element = document.createElement('div');
    this.element.className = 'motor-single';

    const infoContainer = document.createElement('div');
    infoContainer.className = 'motor-info';
    this.element.appendChild(infoContainer);

    this.torqueCheck = document.createElement('input');
    this.torqueCheck.type = 'checkbox';
    this.torqueCheck.className = 'motor-torque-checkbox';
    this.setupTorqueCheck();
    infoContainer.appendChild(this.torqueCheck);

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
    this.stateSlider.setValue(status.position, status.positionLimit);
    this.currentLabel.textContent = status.current.toFixed(1) + 'mA';
    this.loadLabel.textContent = status.load + '';

    // Only update the slider if the user hasn't touched it recently.
    if (performance.now() - this.lastUserChange > 5000) {
      this.targetSlider.setValue(status.targetPos, status.positionLimit);
    } else {
      this.targetSlider.setValue(
        this.targetSlider.value(),
        status.positionLimit,
      );
    }
  }

  private setupTorqueCheck() {
    const loadCls = 'motor-torque-checkbox-loading';
    this.torqueCheck.classList.add(loadCls);
    apiRequest<boolean[]>(`torque?motor=${this.name}`).then((value) => {
      this.torqueCheck.classList.remove(loadCls);
      this.torqueCheck.checked = value[0];
    });
    this.torqueCheck.addEventListener('input', () => {
      this.torqueCheck.classList.add(loadCls);
      const tv = this.torqueCheck.checked ? '1' : '0';
      apiRequest<any>(`torque?motor=${this.name}&enabled=${tv}`).then((_) => {
        this.torqueCheck.classList.remove(loadCls);
      });
    });
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
    this.slider.max = '4095';
    this.slider.step = '1';
    this.slider.className = clsName;

    this.element = document.createElement('div');
    this.element.className = 'motor-slider-container';
    this.element.appendChild(this.slider);

    this.label = document.createElement('label');
    this.label.className = 'motor-slider-label';
    this.element.appendChild(this.label);

    this.slider.addEventListener('input', () => this.updateLabel());
  }

  private updateLabel() {
    const angle = (this.slider.valueAsNumber - 2048) * (360 / 4096);
    this.label.textContent = Math.round(angle) + '°';
  }

  public value(): number {
    return this.slider.valueAsNumber;
  }

  public setValue(value: number, limit: PositionLimit) {
    this.slider.min = '' + limit.min;
    this.slider.max = '' + limit.max;
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

  async moveMotor(motor: string, pos: number) {
    await apiRequest(
      `move?motor=${encodeURIComponent(motor)}&pos=${encodeURIComponent(pos + '')}`,
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
