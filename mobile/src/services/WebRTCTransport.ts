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
  // Multi-listener pattern: Set-based so FeedScreen + SessionScreen coexist.
  private alertHandlers = new Set<(alert: Alert) => void>();
  private statusHandlers = new Set<(s: TransportStatus) => void>();
  private lastSeq = 0;
  private pollTimer: ReturnType<typeof setInterval> | null = null;
  private stopped = false;
  private queuedIce: RTCIceCandidate[] = [];
  private messageQueue: Record<string, unknown>[] = [];

  status: TransportStatus = 'disconnected';

  constructor(
    private signalingUrl: string,
    private sessionCode: string,
  ) {}

  async connect(): Promise<void> {
    this.status = 'connecting';
    this.statusHandlers.forEach(h => h(this.status));
    this.stopped = false;

    // 1. Join session via HTTP.
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

      const handleOpen = () => {
        this.status = 'connected';
        this.statusHandlers.forEach(h => h(this.status));
        // Flush queued messages.
        for (const msg of this.messageQueue) {
          this.dc?.send(JSON.stringify(msg));
        }
        this.messageQueue = [];
      };

      this.dc.onmessage = e => {
        try {
          const alert: Alert = JSON.parse(typeof e.data === 'string' ? e.data : '');
          this.alertHandlers.forEach(h => h(alert));
        } catch {
          // skip
        }
      };
      this.dc.onopen = handleOpen;
      this.dc.onclose = () => {
        this.status = 'disconnected';
        this.statusHandlers.forEach(h => h(this.status));
      };

      // Fix: if channel is already open, trigger open handler immediately.
      if (this.dc.readyState === 'open') {
        handleOpen();
      }
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
        break;
      case 'sdp': {
        const sdp = new RTCSessionDescription({
          type: m.sdpType === 'answer' ? 'answer' : 'offer',
          sdp: m.sdp,
        });
        await this.pc?.setRemoteDescription(sdp);
        for (const c of this.queuedIce) {
          try { await this.pc?.addIceCandidate(c); } catch {}
        }
        this.queuedIce = [];
        if (sdp.type === 'offer') {
          const answer = await this.pc?.createAnswer();
          await this.pc?.setLocalDescription(answer!);
          this.sendSignal({type: 'sdp', sdpType: 'answer', sdp: answer!.sdp});
        }
        break;
      }
      case 'ice':
        if (m.candidate) {
          const c = new RTCIceCandidate({
            candidate: m.candidate,
            sdpMid: m.sdpMid,
            sdpMLineIndex: m.sdpMLineIndex,
          });
          if (!this.pc?.remoteDescription) {
            this.queuedIce.push(c);
          } else {
            try { await this.pc?.addIceCandidate(c); } catch {}
          }
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
      // ignore
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
    this.statusHandlers.forEach(h => h(this.status));
    fetch(`${this.signalingUrl}/leave`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({code: this.sessionCode, role: 'mobile'}),
    }).catch(() => {});
  }

  // Multi-listener: returns cleanup function.
  onStatusChange(handler: (status: TransportStatus) => void): () => void {
    this.statusHandlers.add(handler);
    return () => { this.statusHandlers.delete(handler); };
  }

  onAlert(handler: (alert: Alert) => void): () => void {
    this.alertHandlers.add(handler);
    return () => { this.alertHandlers.delete(handler); };
  }

  sendMessage(payload: Record<string, unknown>): void {
    if (this.dc?.readyState === 'open') {
      this.dc.send(JSON.stringify(payload));
    } else {
      this.messageQueue.push(payload);
    }
  }
}
