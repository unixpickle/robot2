import {CameraView} from './camera';

class App {
  public element: HTMLElement;
  public camContainer: HTMLElement;
  public topCam: CameraView;
  public bottomCam: CameraView;

  constructor() {
    this.element = document.getElementById('container') as HTMLElement;
    this.camContainer = this.element.getElementsByClassName('camera-container')[0] as HTMLElement;
    this.topCam = new CameraView('top');
    this.bottomCam = new CameraView('bottom');
    this.camContainer.appendChild(this.topCam.element);
    this.camContainer.appendChild(this.bottomCam.element);
  }
}

try {
  (window as any).app = new App();
} catch (e) {
  alert(e);
}
