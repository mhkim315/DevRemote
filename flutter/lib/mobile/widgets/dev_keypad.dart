import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../../common.dart';
import '../../models/input_model.dart';

class DevKeypad extends StatefulWidget {
  final InputModel inputModel;
  final VoidCallback? onClose;
  final VoidCallback? onToggleKeyboard;

  const DevKeypad({Key? key, required this.inputModel, this.onClose, this.onToggleKeyboard}) : super(key: key);

  @override
  _DevKeypadState createState() => _DevKeypadState();
}

class _DevKeypadState extends State<DevKeypad> {

  // Toggle modifiers
  void _toggleModifier(String modifier) {
    setState(() {
      switch (modifier) {
        case 'Cmd':
          widget.inputModel.command = !widget.inputModel.command;
          break;
        case 'Ctrl':
          widget.inputModel.ctrl = !widget.inputModel.ctrl;
          break;
        case 'Alt':
          widget.inputModel.alt = !widget.inputModel.alt;
          break;
        case 'Shift':
          widget.inputModel.shift = !widget.inputModel.shift;
          break;
      }
    });
  }

  // Send a simple key (uses current toggled modifiers)
  void _sendKey(String key) {
    widget.inputModel.inputKey(key, press: true);
  }

  // Send a macro (temporarily overrides modifiers)
  void _sendMacro(String key, {bool cmd = false, bool ctrl = false, bool alt = false, bool shift = false}) {
    // Save current state
    bool oldCmd = widget.inputModel.command;
    bool oldCtrl = widget.inputModel.ctrl;
    bool oldAlt = widget.inputModel.alt;
    bool oldShift = widget.inputModel.shift;

    // Apply macro state
    widget.inputModel.command = cmd;
    widget.inputModel.ctrl = ctrl;
    widget.inputModel.alt = alt;
    widget.inputModel.shift = shift;

    // Send key
    widget.inputModel.inputKey(key, press: true);

    // Restore state
    setState(() {
      widget.inputModel.command = oldCmd;
      widget.inputModel.ctrl = oldCtrl;
      widget.inputModel.alt = oldAlt;
      widget.inputModel.shift = oldShift;
    });
  }

  void _syncAndPaste() async {
    // 1. Sync phone clipboard to PC
    gFFI.invokeMethod("try_sync_clipboard");
    
    // 2. Wait a moment for sync to propagate to PC (increased to 800ms per user feedback)
    await Future.delayed(Duration(milliseconds: 800));
    
    // 3. Send Ctrl+V
    _sendMacro('VK_V', ctrl: true);
  }

  void _toggleZoomLock() {
    setState(() {
      gFFI.ffiModel.zoomLock = !gFFI.ffiModel.zoomLock;
    });
  }

  Widget _buildModifierButton(String label, bool isActive) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 2.0),
      child: Material(
        color: isActive ? MyTheme.accent : Colors.grey[800],
        borderRadius: BorderRadius.circular(4.0),
        child: InkWell(
          onTap: () => _toggleModifier(label),
          child: Container(
            padding: EdgeInsets.symmetric(horizontal: 10.0, vertical: 8.0),
            child: Text(
              label,
              style: TextStyle(color: Colors.white, fontWeight: FontWeight.bold, fontSize: 12),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildKeyButton(String label, String keyToSend, {double width = 40.0}) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 2.0),
      child: Material(
        color: Colors.grey[700],
        borderRadius: BorderRadius.circular(4.0),
        child: InkWell(
          onTap: () => _sendKey(keyToSend),
          child: Container(
            width: width,
            alignment: Alignment.center,
            padding: EdgeInsets.symmetric(horizontal: 4.0, vertical: 8.0),
            child: Text(
              label,
              style: TextStyle(color: Colors.white, fontSize: 12),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildMacroButton(String label, String keyToSend, {bool cmd = false, bool ctrl = false, bool alt = false, bool shift = false}) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 2.0),
      child: Material(
        color: Colors.blueGrey[700],
        borderRadius: BorderRadius.circular(4.0),
        child: InkWell(
          onTap: () => _sendMacro(keyToSend, cmd: cmd, ctrl: ctrl, alt: alt, shift: shift),
          child: Container(
            padding: EdgeInsets.symmetric(horizontal: 8.0, vertical: 8.0),
            child: Text(
              label,
              style: TextStyle(color: Colors.amberAccent, fontWeight: FontWeight.bold, fontSize: 12),
            ),
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      height: 40,
      width: double.infinity,
      color: Colors.black87,
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Row(
          children: [
            SizedBox(width: 4),
            // DevRemote Toggles
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 2.0),
              child: Material(
                color: gFFI.ffiModel.zoomLock ? Colors.redAccent : Colors.grey[800],
                borderRadius: BorderRadius.circular(4.0),
                child: InkWell(
                  onTap: _toggleZoomLock,
                  child: Container(
                    padding: EdgeInsets.symmetric(horizontal: 8.0, vertical: 8.0),
                    child: Text(
                      '🔒 Lock',
                      style: TextStyle(color: Colors.white, fontWeight: FontWeight.bold, fontSize: 12),
                    ),
                  ),
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 2.0),
              child: Material(
                color: Colors.blueAccent[700],
                borderRadius: BorderRadius.circular(4.0),
                child: InkWell(
                  onTap: widget.onToggleKeyboard,
                  child: Container(
                    padding: EdgeInsets.symmetric(horizontal: 8.0, vertical: 8.0),
                    child: Text(
                      '⌨️ Keyboard',
                      style: TextStyle(color: Colors.white, fontWeight: FontWeight.bold, fontSize: 12),
                    ),
                  ),
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 2.0),
              child: Material(
                color: Colors.red[900],
                borderRadius: BorderRadius.circular(4.0),
                child: InkWell(
                  onTap: widget.onClose,
                  child: Container(
                    padding: EdgeInsets.symmetric(horizontal: 8.0, vertical: 8.0),
                    child: Text(
                      '🔌 Close',
                      style: TextStyle(color: Colors.white, fontWeight: FontWeight.bold, fontSize: 12),
                    ),
                  ),
                ),
              ),
            ),
            Container(width: 1, height: 20, color: Colors.grey, margin: EdgeInsets.symmetric(horizontal: 4)),
            // Modifiers
            _buildModifierButton('Cmd', widget.inputModel.command),
            _buildModifierButton('Ctrl', widget.inputModel.ctrl),
            _buildModifierButton('Alt', widget.inputModel.alt),
            _buildModifierButton('Shift', widget.inputModel.shift),
            Container(width: 1, height: 20, color: Colors.grey, margin: EdgeInsets.symmetric(horizontal: 4)),
            // Macros
            _buildMacroButton('Copy', 'VK_C', cmd: true),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 2.0),
              child: Material(
                color: Colors.orangeAccent[700],
                borderRadius: BorderRadius.circular(4.0),
                child: InkWell(
                  onTap: _syncAndPaste,
                  child: Container(
                    padding: EdgeInsets.symmetric(horizontal: 8.0, vertical: 8.0),
                    child: Text(
                      '📋 Paste',
                      style: TextStyle(color: Colors.white, fontWeight: FontWeight.bold, fontSize: 12),
                    ),
                  ),
                ),
              ),
            ),
            _buildMacroButton('Undo', 'VK_Z', cmd: true),
            _buildMacroButton('Save', 'VK_S', cmd: true),
            _buildMacroButton('S+Tab', 'VK_TAB', shift: true),
            Container(width: 1, height: 20, color: Colors.grey, margin: EdgeInsets.symmetric(horizontal: 4)),
            // Special Keys
            _buildKeyButton('Esc', 'VK_ESCAPE', width: 40),
            _buildKeyButton('Tab', 'VK_TAB', width: 40),
            _buildKeyButton('Space', 'VK_SPACE', width: 50),
            _buildKeyButton('Enter', 'VK_RETURN', width: 50),
            Container(width: 1, height: 20, color: Colors.grey, margin: EdgeInsets.symmetric(horizontal: 4)),
            // Arrows
            _buildKeyButton('←', 'VK_LEFT', width: 30),
            _buildKeyButton('↓', 'VK_DOWN', width: 30),
            _buildKeyButton('↑', 'VK_UP', width: 30),
            _buildKeyButton('→', 'VK_RIGHT', width: 30),
            SizedBox(width: 4),
          ],
        ),
      ),
    );
  }
}
