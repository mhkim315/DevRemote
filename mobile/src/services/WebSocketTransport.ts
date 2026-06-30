import type {Transport, TransportStatus, Alert} from './types';

export class WebSocketTransport implements Transport {
  private ws: WebSocket | null = null;
  private alertHandler: ((alert: Alert) => void) | null = null;
  private statusHandler: ((s: TransportStatus) => void) | null = null;

  status: TransportStatus = 'disconnected';

  constructor(private url: string) {}

  connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      this.status = 'connecting';
      this.statusHandler?.(this.status);

      const socket = new WebSocket(this.url);

      const timeout = setTimeout(() => {
        socket.close();
        this.status = 'disconnected';
        this.statusHandler?.(this.status);
        reject(new Error('Connection timeout'));
      }, 5000);

      socket.onopen = () => {
        clearTimeout(timeout);
        this.status = 'connected';
        this.statusHandler?.(this.status);
        resolve();
      };

      socket.onmessage = event => {
        try {
          const alert: Alert = JSON.parse(event.data);
          this.alertHandler?.(alert);
        } catch {
          // skip
        }
      };

      socket.onerror = () => {
        clearTimeout(timeout);
        this.status = 'disconnected';
        this.statusHandler?.(this.status);
      };

      socket.onclose = () => {
        this.status = 'disconnected';
        this.statusHandler?.(this.status);
      };

      this.ws = socket;
    });
  }

  disconnect(): void {
    this.ws?.close();
    this.ws = null;
  }

  onStatusChange(handler: (status: TransportStatus) => void): void {
    this.statusHandler = handler;
  }

  onAlert(handler: (alert: Alert) => void): void {
    this.alertHandler = handler;
  }

  sendMessage(payload: Record<string, unknown>): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(payload));
    }
  }
}
