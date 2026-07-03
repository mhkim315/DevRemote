import React, { useState } from 'react';
import { Alert, StyleSheet, View, Text, TextInput, TouchableOpacity, AppState, Platform, Image } from 'react-native';
import { supabase } from '../lib/supabase';
import { SafeAreaView } from 'react-native-safe-area-context';

AppState.addEventListener('change', (state) => {
  if (state === 'active') {
    supabase.auth.startAutoRefresh();
  } else {
    supabase.auth.stopAutoRefresh();
  }
});

export default function AuthScreen() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);

  async function signInWithEmail() {
    setLoading(true);
    const { error } = await supabase.auth.signInWithPassword({
      email: email,
      password: password,
    });

    if (error) Alert.alert('Login Failed', error.message);
    setLoading(false);
  }

  async function signUpWithEmail() {
    setLoading(true);
    const {
      data: { session },
      error,
    } = await supabase.auth.signUp({
      email: email,
      password: password,
    });

    if (error) Alert.alert('Signup Failed', error.message);
    else if (!session) Alert.alert('Success', 'Please check your inbox for email verification!');
    else Alert.alert('Success', 'Account created and logged in!');
    setLoading(false);
  }

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.header}>
        <Image source={require('../../assets/logo.png')} style={styles.logo} />
        <Text style={styles.title}>POKIT</Text>
        <Text style={styles.subtitle}>AI CODING IN YOUR POCKET</Text>
      </View>

      <View style={styles.form}>
        <View style={styles.inputContainer}>
          <Text style={styles.label}>EMAIL</Text>
          <TextInput
            style={styles.input}
            onChangeText={(text) => setEmail(text)}
            value={email}
            placeholder="DEVELOPER@EXAMPLE.COM"
            placeholderTextColor="#5a5a5f"
            autoCapitalize="none"
            keyboardType="email-address"
          />
        </View>
        <View style={styles.inputContainer}>
          <Text style={styles.label}>PASSWORD</Text>
          <TextInput
            style={styles.input}
            onChangeText={(text) => setPassword(text)}
            value={password}
            secureTextEntry={true}
            placeholder="••••••••"
            placeholderTextColor="#5a5a5f"
            autoCapitalize="none"
          />
        </View>

        <TouchableOpacity 
          style={[styles.button, styles.primaryButton]} 
          disabled={loading} 
          onPress={signInWithEmail}
        >
          <Text style={styles.buttonText}>{loading ? 'LOADING...' : 'SIGN IN'}</Text>
        </TouchableOpacity>

        <TouchableOpacity 
          style={[styles.button, styles.secondaryButton]} 
          disabled={loading} 
          onPress={signUpWithEmail}
        >
          <Text style={styles.secondaryButtonText}>CREATE ACCOUNT</Text>
        </TouchableOpacity>
      </View>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#000000',
    padding: 24,
    justifyContent: 'center',
  },
  header: {
    alignItems: 'center',
    marginBottom: 48,
  },
  logo: {
    width: 120,
    height: 120,
    resizeMode: 'contain',
    marginBottom: 24,
  },
  title: {
    fontSize: 60,
    fontWeight: '800',
    color: '#ffffff',
    letterSpacing: 1.6,
    marginBottom: 8,
    fontFamily: Platform.OS === 'ios' ? 'HelveticaNeue-CondensedBold' : 'sans-serif-condensed',
  },
  subtitle: {
    fontSize: 13,
    color: '#45EBE9',
    fontWeight: '700',
    letterSpacing: 1.17,
  },
  form: {
    width: '100%',
  },
  inputContainer: {
    marginBottom: 24,
  },
  label: {
    color: '#1E91B3',
    fontSize: 12,
    fontWeight: '700',
    letterSpacing: 0.96,
    marginBottom: 8,
    marginLeft: 4,
  },
  input: {
    backgroundColor: '#000000',
    borderWidth: 1,
    borderColor: '#1E91B3',
    borderRadius: 4,
    color: '#ffffff',
    paddingHorizontal: 16,
    paddingVertical: Platform.OS === 'ios' ? 14 : 10,
    fontSize: 16,
  },
  button: {
    borderRadius: 32,
    paddingVertical: 18,
    alignItems: 'center',
    marginTop: 12,
  },
  primaryButton: {
    backgroundColor: 'transparent',
    borderWidth: 1,
    borderColor: '#45EBE9',
  },
  secondaryButton: {
    backgroundColor: 'transparent',
    borderWidth: 1,
    borderColor: '#1E91B3',
    marginTop: 16,
  },
  buttonText: {
    color: '#ffffff',
    fontSize: 13,
    fontWeight: '700',
    letterSpacing: 1.17,
  },
  secondaryButtonText: {
    color: '#ffffff',
    fontSize: 13,
    fontWeight: '700',
    letterSpacing: 1.17,
  },
});
