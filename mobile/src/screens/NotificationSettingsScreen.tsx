import React from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

// A dedicated recovery screen for the server's insufficient_permission result.
// It deliberately does not attempt to grant authority; pairing/device settings
// remain the only place permissions can change.
export default function NotificationSettingsScreen() {
  return <SafeAreaView style={styles.container}>
    <View style={styles.card}>
      <Text style={styles.title}>PERMISSION REQUIRED</Text>
      <Text style={styles.body}>This paired device cannot perform the requested action. Review the device pairing and permissions, then try again.</Text>
    </View>
  </SafeAreaView>;
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#000', justifyContent: 'center', padding: 24 },
  card: { borderWidth: 1, borderColor: '#f85149', borderRadius: 12, padding: 20, backgroundColor: '#13090b' },
  title: { color: '#f85149', fontWeight: '800', fontSize: 18, marginBottom: 10 },
  body: { color: '#c9d1d9', fontSize: 14, lineHeight: 21 },
});
