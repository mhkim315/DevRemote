import React, { useState, useEffect } from 'react';
import { StyleSheet, Text, View, Button, Dimensions } from 'react-native';
import { CameraView, Camera } from 'expo-camera';
import { useConnection } from '../lib/connection';
import { config } from '../config';

export default function ConnectScreen() {
  const { connect } = useConnection();
  const [hasPermission, setHasPermission] = useState<boolean | null>(null);
  const [scanned, setScanned] = useState(false);

  useEffect(() => {
    const getCameraPermissions = async () => {
      const { status } = await Camera.requestCameraPermissionsAsync();
      setHasPermission(status === 'granted');
    };

    getCameraPermissions();
  }, []);

  const handleBarCodeScanned = async ({ type, data }: { type: string; data: string }) => {
    if (scanned) return;
    
    // Accept any HTTPS URL
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
          <Button title="Connect to term.fullcount.kr" onPress={async () => {
            await connect('https://term.fullcount.kr');
          }} color="#45EBE9" />
        </View>
        <Text style={styles.manualHint}>Tap above to skip QR. Or scan the terminal QR code.</Text>
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
