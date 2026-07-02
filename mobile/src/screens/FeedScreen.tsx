import React, {useRef, useState, useCallback} from 'react';
import {View, Text, TextInput, StyleSheet, TouchableOpacity} from 'react-native';
import {WebView} from 'react-native-webview';
import {SafeAreaView} from 'react-native-safe-area-context';

const TERM_URL = 'https://term.fullcount.kr/term/';

interface Props { onBack: () => void }
export default function FeedScreen({onBack}: Props) {
  const wv = useRef<WebView>(null);
  const [cmd, setCmd] = useState('');

  const send = useCallback(() => {
    if (cmd.trim() && wv.current) {
      wv.current.injectJavaScript('if(window.ws && window.ws.readyState===1) window.ws.send('+JSON.stringify(cmd.trim()+'\n')+');true;');
      setCmd('');
    }
  }, [cmd]);

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.header}><TouchableOpacity onPress={onBack}><Text style={styles.backBtn}>←</Text></TouchableOpacity></View>
      <WebView ref={wv} source={{uri: TERM_URL}} style={{flex:1,backgroundColor:'#000'}} javaScriptEnabled domStorageEnabled originWhitelist={['*']} />
      <View style={styles.row}>
        <TextInput 
          style={styles.input} 
          placeholder="$ ..." 
          placeholderTextColor="#666" 
          value={cmd} 
          onChangeText={setCmd} 
          onSubmitEditing={send} 
          returnKeyType="send" 
          autoCorrect={false} 
          autoCapitalize="none" 
        />
        <TouchableOpacity onPress={send} style={styles.btn}><Text style={styles.btnT}>Send</Text></TouchableOpacity>
      </View>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container:{flex:1,backgroundColor:'#000'},
  header: {paddingHorizontal: 20, paddingVertical: 12, borderBottomWidth: 1, borderBottomColor: '#1e212b'},
  backBtn: {fontSize: 24, color: '#fff'},
  row:{flexDirection:'row',padding:6,backgroundColor:'#161b22',alignItems:'center'},
  input:{flex:1,backgroundColor:'#21262d',color:'#c9d1d9',borderRadius:6,paddingHorizontal:10,paddingVertical:8,fontSize:14},
  btn:{marginLeft:8,backgroundColor:'#238636',borderRadius:6,paddingHorizontal:14,paddingVertical:8}, btnT:{color:'#fff',fontWeight:'600',fontSize:13},
});
