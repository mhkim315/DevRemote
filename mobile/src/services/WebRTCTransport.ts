import {
  RTCPeerConnection,
  RTCSessionDescription,
  RTCIceCandidate,
  RTCDataChannelEvent,
} from 'react-native-webrtc';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {Transport, TransportStatus, Alert} from './types';

const PEER_KEY_STORAGE = '@devremote/peer_key';

export class WebRTCTransport implements Transport {
  private pc: RTCPeerConnection | null = null;
  private dc: RTCDataChannel | null = null;
  private sig: WebSocket | null = null;
  private alertHandler: ((alert: Alert) => void) | null = null;
  private statusHandler: ((s: TransportStatus) => void) | null = null;
  private pending: Array<{pred: (msg: Record<string, any>) => boolean; resolve: () => void}> = [];
  private paired = false;

  status: TransportStatus = 'disconnected';

  constructor(
    private signalingUrl: string,
    private sessionCode: string,
  ) {}

  async connect(): Promise<void> {
    this.status = 'connecting';
    this.statusHandler?.(this.status);

    const peerKey = await AsyncStorage.getItem(PEER_KEY_STORAGE);

    // 1. Connect to signaling server.
    console.log('[DevRemote] connecting to:', this.signalingUrl);
    this.sig = new WebSocket(this.signalingUrl);
    this.sig.onerror = (e) => {
      console.log('[DevRemote] WS error:', JSON.stringify(e));
    };
    try {
      await this.waitForSigOpen();
      console.log('[DevRemote] WS open');
    } catch (err: any) {
      console.log('[DevRemote] WS fail:', err?.message || String(err));
      this.status = 'disconnected';
      this.statusHandler?.(this.status);
      throw err;
    }

    // 2. Handle all signaling messages.
    this.sig.onmessage = (event: MessageEvent) => {
      const m = JSON.parse(typeof event.data === 'string' ? event.data : '');
      // Give pending waiters first shot, remove matched ones.
      for (let i = this.pending.length - 1; i >= 0; i--) {
        if (this.pending[i].pred(m)) {
          this.pending[i].resolve();
          this.pending.splice(i, 1);
          return;
        }
      }
      // Otherwise, handle normally.
      this.handleSignaling(m);
    };

    // 3. Join as mobile.
    this.sig.send(JSON.stringify(
      peerKey
        ? {type: 'join', role: 'mobile', key: peerKey}
        : {type: 'join', role: 'mobile', code: this.sessionCode},
    ));

    // 4. Wait for joined + optional key + paired.
    await this.waitFor(m => m.type === 'joined' || m.type === 'error');
    await this.waitFor(m => {
      if (m.type === 'key' && m.key) {
        AsyncStorage.setItem(PEER_KEY_STORAGE, m.key);
        return true;
      }
      if (m.type === 'paired' || m.type === 'error') return true;
      return false;
    });
    await this.waitFor(m => m.type === 'paired' || m.type === 'error');

    this.paired = true;

    // 5. Create peer connection.
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
        this.sig?.send(JSON.stringify({
          type: 'ice',
          candidate: event.candidate.candidate,
          sdpMid: event.candidate.sdpMid,
          sdpMLineIndex: event.candidate.sdpMLineIndex,
        }));
      }
    };
  }

  private handleSignaling(m: Record<string, any>) {
    switch (m.type) {
      case 'sdp': {
        const sdp = new RTCSessionDescription({
          type: m.sdp?.type || 'offer',
          sdp: m.sdp,
        });
        this.pc?.setRemoteDescription(sdp).then(() => {
          if (sdp.type === 'offer') {
            this.pc?.createAnswer().then(answer => {
              this.pc?.setLocalDescription(answer);
              this.sig?.send(JSON.stringify({type: 'sdp', sdpType: 'answer', sdp: answer.sdp}));
            }).catch(() => {});
          }
        }).catch(() => {});
        break;
      }
      case 'ice':
        if (this.pc && m.candidate) {
          this.pc.addIceCandidate(
            new RTCIceCandidate({
              candidate: m.candidate,
              sdpMid: m.sdpMid,
              sdpMLineIndex: m.sdpMLineIndex,
            }),
          ).catch(() => {});
        }
        break;
      case 'peer_disconnected':
        this.disconnect();
        break;
    }
  }

  private waitFor(predicate: (msg: Record<string, any>) => boolean): Promise<void> {
    return new Promise((resolve, reject) => {
      const t = setTimeout(() => {
        // Remove stale waiter on timeout.
        const idx = this.pending.findIndex(w => w.resolve === r);
        if (idx >= 0) this.pending.splice(idx, 1);
        reject(new Error('Signaling timeout'));
      }, 15000);
      const r = () => { clearTimeout(t); resolve(); };
      this.pending.push({pred: predicate, resolve: r});
    });
  }

  private waitForSigOpen(): Promise<void> {
    return new Promise((resolve, reject) => {
      const t = setTimeout(() => reject(new Error('timeout')), 10000);
      this.sig!.onopen = () => {
        clearTimeout(t);
        resolve();
      };
      this.sig!.onerror = () => {
        clearTimeout(t);
        reject(new Error('Signaling connect failed'));
      };
    });
  }

  disconnect(): void {
    this.dc?.close();
    this.pc?.close();
    this.sig?.close();
    this.dc = null;
    this.pc = null;
    this.sig = null;
    this.status = 'disconnected';
    this.statusHandler?.(this.status);
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
