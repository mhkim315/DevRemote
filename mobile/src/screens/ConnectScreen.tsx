import React, { useState, useEffect } from 'react';
import { StyleSheet, Text, TextInput, View, Button, TouchableOpacity, Dimensions } from 'react-native';
import { CameraView, Camera } from 'expo-camera';
import { useConnection } from '../lib/connection';
import { isPairingQR, pairFromScannedQR, runScan } from '../lib/connectPairing';
import { canonicalOrigin, canonicalLocalTestOrigin } from '../lib/authMode';

const DEFAULT_OPERATIONAL_URL = 'https://term.fullcount.kr';
const LOCAL_TEST_DEFAULT_URL = 'http://localhost:9172';

export default function ConnectScreen({ onPaired, localTest }: {
  onPaired?: () => Promise<boolean>;
  localTest?: boolean;
} = {}) {
  const { connect, connectionError } = useConnection();
  const [hasPermission, setHasPermission] = useState<boolean | null>(null);
  const [scanned, setScanned] = useState(false);
  const [operationalURL, setOperationalURL] = useState(localTest ? LOCAL_TEST_DEFAULT_URL : DEFAULT_OPERATIONAL_URL);
  const [urlError, setURLError] = useState('');

  useEffect(() => {
    const getCameraPermissions = async () => {
      const { status } = await Camera.requestCameraPermissionsAsync();
      setHasPermission(status === 'granted');
    };
    getCameraPermissions();
  }, []);

  const validateURL = (url: string) => {
    const trimmed = url.trim();
    if (!trimmed) { setURLError(''); return true; }
    // Local E2E test path: permit HTTP localhost when explicit_local_dev + debug build
    if (localTest) {
      const { error } = canonicalLocalTestOrigin(trimmed, true, 'explicit_local_dev');
      if (error) { setURLError(error); return false; }
      setURLError('');
      return true;
    }
    const { error } = canonicalOrigin(trimmed);
    if (error) { setURLError(error); return false; }
    setURLError('');
    return true;
  };

  const handleSetURL = (url: string) => {
    const trimmed = url.trim();
    if (trimmed && validateURL(trimmed)) {
      setOperationalURL(trimmed);
      // In local test mode, immediately connect to the daemon after URL set.
      // This triggers ConnectionProvider.probeDaemon() → isConnected → product route.
      if (localTest) {
        connect(trimmed).catch(() => {});
      }
    }
  };

  const handleBarCodeScanned = async ({ type, data }: { type: string; data: string }) => {
    if (scanned) return;
    // PA4: pairing requires a validated operational URL. The QR transport
    // endpoint (LAN HTTP) is only used for cryptographic pairing; the
    // operational origin (HTTPS) is where the device bearer is sent.
    const base = operationalURL.trim();
    const { error } = canonicalOrigin(base);
    if (error) {
      alert('Set a valid HTTPS daemon URL before scanning. ' + error);
      return;
    }
    await runScan({
      data,
      isPairing: isPairingQR,
      loadModules: async () => {
        const [{ pairAndSave }, { createPokitDeviceKey }] = await Promise.all([
          import('../lib/pairAndSave'),
          import('../../modules/pokit-device-key'),
        ]);
        return {
          pairFromScannedQR,
          pairAndSave,
          createDeviceKey: createPokitDeviceKey,
          operationalBaseURL: base,
        };
      },
      connect: (url) => connect(url),
      onPaired,
      setScanned,
      notifyError: (msg) => alert(msg),
    });
  };

  if (hasPermission === null) {
    return (
      <View style={styles.container}>
        <Text style={styles.text}>Requesting camera permission...</Text>
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
      <Text style={styles.subText}>Make sure this phone and your Mac are on the same WiFi network.</Text>

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
          {urlError ? (
            <Text style={styles.error}>{urlError}</Text>
          ) : null}
          <Text style={styles.inputLabel}>Daemon URL (HTTPS required)</Text>
          <TextInput
            style={styles.input}
            placeholder={DEFAULT_OPERATIONAL_URL}
            placeholderTextColor="#666"
            value={operationalURL}
            onChangeText={(t) => { setOperationalURL(t); validateURL(t); }}
            autoCapitalize="none"
            autoCorrect={false}
          />
          <TouchableOpacity style={styles.setUrlBtn} onPress={() => handleSetURL(operationalURL)}>
            <Text style={styles.setUrlBtnText}>SET DAEMON URL</Text>
          </TouchableOpacity>
          <Text style={styles.urlNote}>This URL is used after QR pairing succeeds. Scan the QR code from your terminal to pair.</Text>
        </View>
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
    flex: 1, backgroundColor: '#000', alignItems: 'center',
    justifyContent: 'center', padding: 20,
  },
  error: {
    color: '#f85149', fontSize: 13, textAlign: 'center',
    marginBottom: 12, paddingHorizontal: 20,
  },
  inputLabel: {
    color: '#8b949e', fontSize: 12, marginBottom: 6, fontWeight: '600',
  },
  input: {
    backgroundColor: '#1C1C1E', color: '#fff', borderRadius: 8,
    padding: 10, fontSize: 14, marginBottom: 12,
    borderWidth: 1, borderColor: '#0D2D45',
  },
  setUrlBtn: {
    backgroundColor: '#0D2D45', borderRadius: 8,
    padding: 12, alignItems: 'center', marginBottom: 8,
  },
  setUrlBtnText: {
    color: '#45EBE9', fontWeight: '800', fontSize: 14,
  },
  urlNote: {
    color: '#666', fontSize: 11, textAlign: 'center',
    marginTop: 4, paddingHorizontal: 10,
  },
  title: {
    color: '#fff', fontSize: 28, fontWeight: 'bold',
    marginBottom: 10, marginTop: 60,
  },
  subtitle: {
    color: '#aaa', fontSize: 16, textAlign: 'center', marginBottom: 40,
  },
  text: { color: '#fff', fontSize: 18 },
  subText: { color: '#888', fontSize: 14, textAlign: 'center', marginTop: 10 },
  cameraContainer: {
    width: width, height: width, overflow: 'hidden',
    position: 'relative', borderRadius: 20,
  },
  overlay: {
    ...StyleSheet.absoluteFill, justifyContent: 'center', alignItems: 'center',
  },
  scanBox: {
    width: scanBoxSize, height: scanBoxSize,
    borderWidth: 2, borderColor: '#4ade80',
    backgroundColor: 'transparent', borderRadius: 10,
  },
  manualContainer: { marginTop: 20, alignItems: 'center' },
  manualBtn: { marginBottom: 10 },
});
