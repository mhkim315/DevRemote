import {
  RTCPeerConnection,
  RTCSessionDescription,
  RTCIceCandidate,
  RTCDataChannelEvent,
} from 'react-native-webrtc';
import type {Transport, TransportStatus, Alert} from './types';

const POLL_MS = 500;

interface SignalingMsg {
  seq: number;
  msg: Record<string, any>;
}

export class WebRTCTransport implements Transport {
  private pc: RTCPeerConnection | null = null;
  private dc: RTCDataChannel | null = null;
  private alertHandler: ((alert: Alert) => void) | null = null;
  private statusHandler: ((s: TransportStatus) => void) | null = null;
  private lastSeq = 0;
  private pollTimer: ReturnType<typeof setInterval> | null = null;
  private stopped = false;

  status: TransportStatus = 'disconnected';

  constructor(
    private signalingUrl: string,
    private sessionCode: string,
  ) {}

  async connect(): Promise<void> {
    this.status = 'connecting';
    this.statusHandler?.(this.status);
    this.stopped = false;

    // 1. Join session via HTTP — always use code, not stored key.
    // The code is the unique session identifier. Daemon creates the session.
    console.log('[DevRemote] joining:', this.signalingUrl);
    const joinResp = await fetch(`${this.signalingUrl}/join`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({code: this.sessionCode, role: 'mobile'}),
    });
    const joinData = await joinResp.json();
    console.log('[DevRemote] join response:', JSON.stringify(joinData));

    if (joinData.status !== 'ok') {
      throw new Error(joinData.message || 'Join failed');
    }

    // 2. Create peer connection.
    this.pc = new RTCPeerConnection({
      iceServers: [{urls: 'stun:stun.l.google.com:19302'}],
    });

    this.pc.ondatachannel = (event: RTCDataChannelEvent) => {
      this.dc = event.channel;
      this.dc.onmessage = e => {
        try {
          const alert: Alert = JSON.parse(typeof e.data === 'string' ? e.data : '');
          this.alertHandler?.(alert);
        } catch {
          // skip
        }
      };
      this.dc.onopen = () => {
        this.status = 'connected';
        this.statusHandler?.(this.status);
      };
      this.dc.onclose = () => {
        this.status = 'disconnected';
        this.statusHandler?.(this.status);
      };
    };

    this.pc.onicecandidate = event => {
      if (event.candidate) {
        this.sendSignal({
          type: 'ice',
          candidate: event.candidate.candidate,
          sdpMid: event.candidate.sdpMid,
          sdpMLineIndex: event.candidate.sdpMLineIndex,
        });
      }
    };

    // 3. Start polling for signaling messages.
    this.pollTimer = setInterval(() => this.poll(), POLL_MS);
    this.poll();
  }

  private async poll(): Promise<void> {
    if (this.stopped) return;
    try {
      const resp = await fetch(
        `${this.signalingUrl}/poll?code=${this.sessionCode}&role=mobile&since=${this.lastSeq}`,
      );
      const data = await resp.json();
      const msgs: SignalingMsg[] = data.messages || [];
      if (msgs.length > 0) {
        this.lastSeq = data.since || this.lastSeq;
      }
      // Process SDP first (required before ICE candidates).
      const sorted = [...msgs].sort((a, b) => {
        if (a.msg.type === 'sdp') return -1;
        if (b.msg.type === 'sdp') return 1;
        return 0;
      });
      for (const sm of sorted) {
        await this.handleSignal(sm.msg);
      }
    } catch {
      // retry on next poll
    }
  }

  private async handleSignal(m: Record<string, any>): Promise<void> {
    switch (m.type) {
      case 'paired':
        // Other peer has joined.
        break;
      case 'sdp': {
        const sdp = new RTCSessionDescription({
          type: m.sdpType === 'answer' ? 'answer' : 'offer',
          sdp: m.sdp,
        });
        await this.pc?.setRemoteDescription(sdp);
        if (sdp.type === 'offer') {
          const answer = await this.pc?.createAnswer();
          await this.pc?.setLocalDescription(answer!);
          this.sendSignal({type: 'sdp', sdpType: 'answer', sdp: answer!.sdp});
        }
        break;
      }
      case 'ice':
        if (m.candidate) {
          await this.pc?.addIceCandidate(
            new RTCIceCandidate({
              candidate: m.candidate,
              sdpMid: m.sdpMid,
              sdpMLineIndex: m.sdpMLineIndex,
            }),
          );
        }
        break;
      case 'peer_disconnected':
        this.disconnect();
        break;
    }
  }

  private async sendSignal(msg: Record<string, any>): Promise<void> {
    try {
      await fetch(`${this.signalingUrl}/send`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({code: this.sessionCode, role: 'mobile', msg}),
      });
    } catch {
      // ignore send errors
    }
  }

  disconnect(): void {
    this.stopped = true;
    if (this.pollTimer) {
      clearInterval(this.pollTimer);
      this.pollTimer = null;
    }
    this.dc?.close();
    this.pc?.close();
    this.dc = null;
    this.pc = null;
    this.status = 'disconnected';
    this.statusHandler?.(this.status);
    fetch(`${this.signalingUrl}/leave`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({code: this.sessionCode, role: 'mobile'}),
    }).catch(() => {});
  }

  onStatusChange(handler: (status: TransportStatus) => void): void {
    this.statusHandler = handler;
  }

  onAlert(handler: (alert: Alert) => void): void {
    this.alertHandler = handler;
  }

  sendMessage(payload: Record<string, unknown>): void {
    if (this.dc?.readyState === 'open') {
      this.dc.send(JSON.stringify(payload));
    }
  }
}
