import React, { useState, useEffect } from 'react';
import { StyleSheet, Text, TextInput, View, Button, TouchableOpacity, Dimensions } from 'react-native';
import { CameraView, Camera } from 'expo-camera';
import { useConnection } from '../lib/connection';
import { pairThenConnect } from '../lib/authMode';

export default function ConnectScreen({ onPaired }: { onPaired?: () => Promise<boolean> } = {}) {
  const { connect, connectionError } = useConnection();
  const [hasPermission, setHasPermission] = useState<boolean | null>(null);
  const [scanned, setScanned] = useState(false);
  const [manualURL, setManualURL] = useState('');

  useEffect(() => {
    const getCameraPermissions = async () => {
      const { status } = await Camera.requestCameraPermissionsAsync();
      setHasPermission(status === 'granted');
    };

    getCameraPermissions();
  }, []);

  const handleBarCodeScanned = async ({ type, data }: { type: string; data: string }) => {
    if (scanned) return;

    // Detect pairing QR payload: JSON with sessionId.
    let payload: any = null;
    try { payload = JSON.parse(data); } catch { payload = null; }

    if (payload && typeof payload === 'object' && payload.sessionId && payload.hostPubKey) {
      setScanned(true);
      // B4: any failure in pairing install or connection must re-enable the
      // scanner exactly once — never swallow it into a stuck scanned=true state.
      try {
        // Import pairAndSave lazily so ConnectScreen doesn't bundle it eagerly.
        const { pairAndSave } = await import('../lib/pairAndSave');
        const { createPokitDeviceKey } = await import('../../modules/pokit-device-key');
        const dk = createPokitDeviceKey();
        // Operational base URL is the currently set daemon URL (tunnel/remote).
        const { getBaseURL } = await import('../lib/client');
        const base = getBaseURL();
        if (!base) { alert('Set the daemon URL first'); setScanned(false); return; }
        const result = await pairAndSave(data, dk, base);
        if (result.status === 'approved') {
          // Blocker C: install trusted auth state (onPaired) FIRST, then connect
          // ONLY if it succeeded. pairThenConnect calls onReject exactly once on
          // any failure so the scanner is usable again.
          await pairThenConnect({
            onPaired,
            connect: () => connect(base),
            onReject: () => setScanned(false),
          });
        } else {
          alert('Pairing failed: ' + (result.errorDetail || result.status));
          setScanned(false);
        }
      } catch (e: any) {
        alert('Pairing error: ' + (e?.message || String(e)));
        setScanned(false);
      }
      return;
    }

    // Accept any HTTPS URL (legacy or manual connect).
    if (data.startsWith('https://')) {
      setScanned(true);
      await connect(data);
    }
  };

  if (hasPermission === null) {
    return (
      <View style={styles.container}>
        <Text style={styles.text}>Requesting for camera permission...</Text>
      </View>
    );
  }
  if (hasPermission === false) {
    return (
      <View style={styles.container}>
        <Text style={styles.text}>No access to camera.</Text>
        <Text style={styles.subText}>Please enable camera permissions in settings to scan the QR code.</Text>
      </View>
    );
  }

  return (
    <View style={styles.container}>
      <Text style={styles.title}>Welcome to POKIT</Text>
      <Text style={styles.subtitle}>Scan the QR code in your terminal to connect instantly.</Text>
      
      <View style={styles.cameraContainer}>
        <CameraView
          onBarcodeScanned={scanned ? undefined : handleBarCodeScanned}
          barcodeScannerSettings={{
            barcodeTypes: ["qr"],
          }}
          style={StyleSheet.absoluteFill}
        />
        <View style={styles.overlay}>
          <View style={styles.scanBox} />
        </View>
      </View>
      
      <View style={styles.manualContainer}>
        <View style={styles.manualBtn}>
          {connectionError ? (
            <Text style={styles.error}>{connectionError}</Text>
          ) : null}
          <Text style={styles.inputLabel}>Daemon URL</Text>
          <TextInput
            style={styles.input}
            placeholder="http://localhost:9171"
            placeholderTextColor="#666"
            value={manualURL}
            onChangeText={setManualURL}
            autoCapitalize="none"
            autoCorrect={false}
          />
          <TouchableOpacity style={styles.connectBtn} onPress={async () => {
            await connect(manualURL || 'http://localhost:9171');
          }}>
            <Text style={styles.connectBtnText}>CONNECT</Text>
          </TouchableOpacity>
          <View style={{height: 12}} />
          <Button title="Connect to term.fullcount.kr" onPress={async () => {
            await connect('https://term.fullcount.kr');
          }} color="#45EBE9" />
        </View>
        <Text style={styles.manualHint}>Enter daemon URL or scan the terminal QR code.</Text>
      </View>

      {scanned && (
        <Button title={'Tap to Scan Again'} onPress={() => setScanned(false)} />
      )}
    </View>
  );
}

const { width } = Dimensions.get('window');
const scanBoxSize = width * 0.7;

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#000',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 20,
  },
  error: {
    color: '#f85149',
    fontSize: 13,
    textAlign: 'center',
    marginBottom: 12,
    paddingHorizontal: 20,
  },
  inputLabel: {
    color: '#8b949e',
    fontSize: 12,
    marginBottom: 6,
    fontWeight: '600',
  },
  input: {
    backgroundColor: '#1C1C1E',
    color: '#fff',
    borderRadius: 8,
    padding: 10,
    fontSize: 14,
    marginBottom: 12,
    borderWidth: 1,
    borderColor: '#0D2D45',
  },
  connectBtn: {
    backgroundColor: '#39d353',
    borderRadius: 8,
    padding: 12,
    alignItems: 'center',
    marginBottom: 8,
  },
  connectBtnText: {
    color: '#000',
    fontWeight: '800',
    fontSize: 14,
  },
  title: {
    color: '#fff',
    fontSize: 28,
    fontWeight: 'bold',
    marginBottom: 10,
    marginTop: 60,
  },
  subtitle: {
    color: '#aaa',
    fontSize: 16,
    textAlign: 'center',
    marginBottom: 40,
  },
  text: {
    color: '#fff',
    fontSize: 18,
  },
  subText: {
    color: '#888',
    fontSize: 14,
    textAlign: 'center',
    marginTop: 10,
  },
  cameraContainer: {
    width: width,
    height: width,
    overflow: 'hidden',
    position: 'relative',
    borderRadius: 20,
  },
  overlay: {
    ...StyleSheet.absoluteFill,
    justifyContent: 'center',
    alignItems: 'center',
  },
  scanBox: {
    width: scanBoxSize,
    height: scanBoxSize,
    borderWidth: 2,
    borderColor: '#4ade80',
    backgroundColor: 'transparent',
    borderRadius: 10,
  },
  manualContainer: {
    marginTop: 20,
    alignItems: 'center',
  },
  manualBtn: {
    marginBottom: 10,
  },
  manualHint: {
    color: '#666',
    fontSize: 12,
    marginTop: 5,
  },
});
