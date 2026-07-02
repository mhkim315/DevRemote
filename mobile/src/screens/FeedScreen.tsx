import React, {useRef, useState, useCallback} from 'react';
import {View, Text, TextInput, StyleSheet, TouchableOpacity, Platform} from 'react-native';
import {WebView} from 'react-native-webview';
import {SafeAreaView} from 'react-native-safe-area-context';

const TERM_URL = Platform.OS === 'android'
  ? 'http://192.168.219.100:9171/term/'
  : 'http://localhost:9171/term/';

interface Props { onBack: () => void }
export default function FeedScreen({onBack}: Props) {
  const wv = useRef<WebView>(null);
  const [cmd, setCmd] = useState('');
  const send = useCallback(() => {
    if (cmd.trim() && wv.current) {
      wv.current.injectJavaScript(
        'window.ws.send('+JSON.stringify(cmd.trim()+'\n')+');true;'
      );
      setCmd('');
    }
  }, [cmd]);
  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.header}><TouchableOpacity onPress={onBack}><Text style={styles.backBtn}>←</Text></TouchableOpacity></View>
      <WebView ref={wv} source={{uri: TERM_URL}} style={styles.webview} javaScriptEnabled domStorageEnabled originWhitelist={['*']} />
      <View style={styles.row}>
        <TextInput style={styles.input} placeholder="$ ..." placeholderTextColor="#666" value={cmd} onChangeText={setCmd} onSubmitEditing={send} returnKeyType="send" autoCorrect={false} autoCapitalize="none" />
        <TouchableOpacity onPress={send} style={styles.btn}><Text style={styles.btnT}>Send</Text></TouchableOpacity>
      </View>
    </SafeAreaView>
  );
}
const styles = StyleSheet.create({
  container:{flex:1,backgroundColor:'#000'}, header:{paddingHorizontal:12,paddingVertical:6,backgroundColor:'#161b22'},
  backBtn:{color:'#58a6ff',fontSize:14}, webview:{flex:1,backgroundColor:'#000'},
  row:{flexDirection:'row',padding:6,backgroundColor:'#161b22',alignItems:'center'},
  input:{flex:1,backgroundColor:'#21262d',color:'#c9d1d9',borderRadius:6,paddingHorizontal:10,paddingVertical:8,fontSize:14},
  btn:{marginLeft:8,backgroundColor:'#238636',borderRadius:6,paddingHorizontal:14,paddingVertical:8}, btnT:{color:'#fff',fontWeight:'600',fontSize:13},
});
