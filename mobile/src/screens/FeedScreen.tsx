import React, {useRef, useState, useCallback, useEffect, useMemo} from 'react';
import {View, Text, TextInput, StyleSheet, TouchableOpacity, ScrollView, Platform, Keyboard, Modal} from 'react-native';
import {WebView} from 'react-native-webview';
import {SafeAreaView} from 'react-native-safe-area-context';
import * as Clipboard from 'expo-clipboard';

interface Props {
  onBack: () => void;
  session: string;
  token?: string;
}

function jsSend(chars: number[]): string {
  const arr = JSON.stringify(chars);
  return `window.ws.send(String.fromCharCode.apply(null, ${arr}))`;
}

const NORMAL_MACROS: { label: string; chars: number[] }[] = [
  { label: 'Ctrl+C',  chars: [3] },
  { label: 'Esc',     chars: [27] },
  { label: 'Tab',     chars: [9] },
  { label: '↑',       chars: [27, 91, 65] },
  { label: '↓',       chars: [27, 91, 66] },
  { label: '←',       chars: [27, 91, 68] },
  { label: '→',       chars: [27, 91, 67] },
  { label: 'Y',       chars: [121, 13] },
  { label: 'N',       chars: [110, 13] },
  { label: 'Enter',   chars: [13] },
];

const SCROLL_MACROS: { label: string; chars: number[] }[] = [
  { label: '▲ Page Up',    chars: [27, 91, 53, 126] },
  { label: '▼ Page Down',  chars: [27, 91, 54, 126] },
  { label: '↑ Line Up',    chars: [27, 91, 65] },
  { label: '↓ Line Down',  chars: [27, 91, 66] },
  { label: '❌ Exit Scroll',chars: [113] }, // 'q' to exit tmux copy mode
];

export default function FeedScreen({onBack, session, token}: Props) {
  const wv = useRef<any>(null);
  const cmdRef = useRef('');
  const [cmd, setCmd] = useState('');
  const [kbHeight, setKbHeight] = useState(0);
  const [isScrollMode, setIsScrollMode] = useState(false);

  const [copyModalVisible, setCopyModalVisible] = useState(false);
  const [copyText, setCopyText] = useState('');

  const termUrl = useMemo(() => {
    let url = `https://term.fullcount.kr/term/?session=${encodeURIComponent(session)}`;
    if (token) {
      url += `&token=${encodeURIComponent(token)}`;
    }
    return url;
  }, [session, token]);
  const source = useMemo(() => ({uri: termUrl}), [termUrl]);

  useEffect(() => {
    const show = Keyboard.addListener('keyboardDidShow', (e) => {
      setKbHeight(e.endCoordinates.height);
    });
    const hide = Keyboard.addListener('keyboardDidHide', () => {
      setKbHeight(0);
    });
    return () => { show.remove(); hide.remove(); };
  }, []);

  const inject = useCallback((js: string) => {
    if (wv.current) {
      wv.current.injectJavaScript(js + ';true;');
    }
  }, []);

  const doSend = useCallback((text: string) => {
    if (!text || !wv.current) return;
    // Replace newlines with \r to match terminal enter behavior
    const parsedText = text.replace(/\n/g, '\r');
    inject('if(window.ws&&window.ws.readyState===1)window.ws.send('+JSON.stringify(parsedText)+')');
  }, [inject]);

  const send = useCallback(() => {
    doSend(cmd + '\r');
    setCmd('');
  }, [cmd, doSend]);

  const sendMacro = useCallback((chars: number[]) => {
    inject('if(window.ws&&window.ws.readyState===1){'+jsSend(chars)+'}');
  }, [inject]);

  const handleChangeText = useCallback((text: string) => {
    cmdRef.current = text;
    if (text.endsWith('\n')) {
      doSend(text.replace(/\n/g, '') + '\r');
      setCmd('');
    } else {
      setCmd(text);
    }
  }, [doSend]);

  const handleCopyRequest = useCallback(() => {
    inject('if(window.getTerminalText) window.getTerminalText();');
  }, [inject]);

  const handlePasteRequest = useCallback(async () => {
    const text = await Clipboard.getStringAsync();
    if (text) {
      doSend(text);
    }
  }, [doSend]);

  const handleScrollToggle = useCallback(() => {
    if (isScrollMode) {
      // Exit scroll mode ('q')
      inject('if(window.ws&&window.ws.readyState===1){'+jsSend([113])+'}');
      setIsScrollMode(false);
    } else {
      // Enter scroll mode (Ctrl+B, [)
      inject('if(window.ws&&window.ws.readyState===1){'+jsSend([2, 91])+'}');
      setIsScrollMode(true);
    }
  }, [isScrollMode, inject]);

  const onMessage = useCallback((event: any) => {
    try {
      const data = JSON.parse(event.nativeEvent.data);
      if (data.type === 'copy') {
        setCopyText(data.text);
        setCopyModalVisible(true);
      }
    } catch (e) {}
  }, []);

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.flex}>
        <View style={styles.header}>
          <TouchableOpacity onPress={onBack}><Text style={styles.backBtn}>←</Text></TouchableOpacity>
          <Text style={styles.headerTitle}>{session}</Text>
          <View style={{flexDirection: 'row', alignItems: 'center'}}>
            <TouchableOpacity onPress={handleCopyRequest} style={styles.textBtn}><Text style={styles.textBtnText}>Copy</Text></TouchableOpacity>
            <TouchableOpacity onPress={handlePasteRequest} style={styles.textBtn}><Text style={styles.textBtnText}>Paste</Text></TouchableOpacity>
            <TouchableOpacity onPress={handleScrollToggle} style={[styles.textBtn, isScrollMode && {backgroundColor: '#238636', borderColor: '#2ea043'}]}><Text style={[styles.textBtnText, isScrollMode && {color: '#fff'}]}>{isScrollMode ? 'Scroll: ON' : 'Scroll: OFF'}</Text></TouchableOpacity>
            <TouchableOpacity onPress={() => {
              if (wv.current) wv.current.reload();
            }}><Text style={styles.reloadBtn}>↻</Text></TouchableOpacity>
          </View>
        </View>

        <WebView
          ref={wv}
          source={source}
          style={styles.webview}
          javaScriptEnabled
          domStorageEnabled
          originWhitelist={['*']}
          cacheEnabled={false}
          onMessage={onMessage}
        />

        <View style={[styles.macroContainer, isScrollMode && {backgroundColor: '#1b120f', borderTopColor: '#d29922'}]}>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.macroScroll}>
            {(isScrollMode ? SCROLL_MACROS : NORMAL_MACROS).map((m, i) => (
              <TouchableOpacity key={i} style={[styles.macroBtn, isScrollMode && {backgroundColor: '#3d2b1f', borderColor: '#d29922'}]} onPress={() => m.label === '❌ Exit Scroll' ? handleScrollToggle() : sendMacro(m.chars)}>
                <Text style={[styles.macroText, isScrollMode && {color: '#e3b341'}]}>{m.label}</Text>
              </TouchableOpacity>
            ))}
          </ScrollView>
        </View>

        <View style={[styles.inputContainer, {paddingBottom: Math.max(kbHeight, Platform.OS === 'ios' ? 20 : 6)}]}>
          <TextInput
            style={styles.input}
            placeholder="$ type a command..."
            placeholderTextColor="#666"
            value={cmd}
            onChangeText={handleChangeText}
            onSubmitEditing={send}
            returnKeyType="send"
            autoCorrect={false}
            autoCapitalize="none"
            multiline={false}
            blurOnSubmit={false}
          />
          <TouchableOpacity onPress={send} style={styles.btn}><Text style={styles.btnT}>Send</Text></TouchableOpacity>
        </View>
      </View>

      <Modal
        visible={copyModalVisible}
        animationType="slide"
        transparent={true}
        onRequestClose={() => setCopyModalVisible(false)}
      >
        <View style={styles.modalBg}>
          <View style={styles.modalContainer}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>Select & Copy Text</Text>
              <TouchableOpacity onPress={() => setCopyModalVisible(false)}>
                <Text style={styles.closeBtn}>Close</Text>
              </TouchableOpacity>
            </View>
            <ScrollView style={styles.modalScroll}>
              <Text style={styles.modalText} selectable={true}>
                {copyText}
              </Text>
            </ScrollView>
            <View style={styles.modalFooter}>
              <Text style={styles.modalHint}>Long press on the text above to select and copy.</Text>
            </View>
          </View>
        </View>
      </Modal>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {flex:1, backgroundColor:'#000000'},
  flex: {flex:1},
  header: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 12, paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#0D2D45',
  },
  headerTitle: {fontSize: 14, color: '#1E91B3', fontWeight: '700', letterSpacing: 0.96},
  backBtn: {fontSize: 24, color: '#45EBE9'},
  reloadBtn: {fontSize: 20, color: '#45EBE9', paddingHorizontal: 4},
  iconBtn: {fontSize: 18},
  textBtn: {backgroundColor: 'transparent', paddingHorizontal: 10, paddingVertical: 4, borderRadius: 32, borderWidth: 1, borderColor: '#1E91B3', marginRight: 10},
  textBtnText: {color: '#1E91B3', fontSize: 12, fontWeight: '700', letterSpacing: 0.96},
  webview: {flex:1, backgroundColor:'#000'},
  macroContainer: { backgroundColor: '#000000', borderTopWidth: 1, borderTopColor: '#0D2D45' },
  macroScroll: { paddingHorizontal: 6, paddingVertical: 6, alignItems: 'center' },
  macroBtn: { backgroundColor: 'transparent', paddingHorizontal: 10, paddingVertical: 6, borderRadius: 32, marginRight: 5, borderWidth: 1, borderColor: '#1E91B3' },
  macroText: { color: '#1E91B3', fontSize: 12, fontWeight: '700', letterSpacing: 0.96 },
  inputContainer: {
    flexDirection: 'row', padding: 6, backgroundColor: '#000000',
    borderTopWidth: 1, borderTopColor: '#0D2D45', alignItems: 'center',
  },
  input: {
    flex:1, backgroundColor: '#000000', color: '#ffffff', borderRadius: 4,
    borderWidth: 1, borderColor: '#1E91B3', paddingHorizontal: 10, paddingVertical: 10, fontSize: 14,
  },
  btn: {marginLeft:8, backgroundColor:'transparent', borderRadius:32, paddingHorizontal:14, paddingVertical:10, borderWidth: 1, borderColor: '#45EBE9'},
  btnT: {color:'#ffffff', fontWeight:'700', fontSize:13, letterSpacing: 1.17},
  modalBg: {flex: 1, backgroundColor: 'rgba(0,0,0,0.8)', justifyContent: 'flex-end'},
  modalContainer: {height: '80%', backgroundColor: '#0D2D45', borderTopLeftRadius: 16, borderTopRightRadius: 16, padding: 16},
  modalHeader: {flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12},
  modalTitle: {color: '#ffffff', fontSize: 16, fontWeight: '800', letterSpacing: 1.2},
  closeBtn: {color: '#45EBE9', fontSize: 13, fontWeight: '700', letterSpacing: 1.17},
  modalScroll: {flex: 1, backgroundColor: '#000000', borderRadius: 4, padding: 12, borderWidth: 1, borderColor: '#1E91B3'},
  modalText: {color: '#45EBE9', fontSize: 13, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace'},
  modalFooter: {marginTop: 12, alignItems: 'center'},
  modalHint: {color: '#1E91B3', fontSize: 12, fontWeight: '700', letterSpacing: 0.96},
});
