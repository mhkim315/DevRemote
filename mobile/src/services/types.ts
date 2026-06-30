export interface Alert {
  sessionId: string;
  toolUseId: string;
  toolName: string;
  description: string;
  question: string;
  timestamp: string;
}

export type TransportStatus = 'connecting' | 'connected' | 'disconnected';

export interface Transport {
  status: TransportStatus;
  connect(): Promise<void>;
  disconnect(): void;
  onStatusChange(handler: (status: TransportStatus) => void): void;
  onAlert(handler: (alert: Alert) => void): void;
  sendMessage(payload: Record<string, unknown>): void;
}
