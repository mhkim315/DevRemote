import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:get/get.dart';

class AgentEvent {
  final String action;
  final String message;
  final String command;

  AgentEvent({required this.action, required this.message, required this.command});

  factory AgentEvent.fromJson(Map<String, dynamic> json) {
    return AgentEvent(
      action: json['action'] ?? '',
      message: json['message'] ?? '',
      command: json['command'] ?? '',
    );
  }
}

class AgentMonitorModel extends ChangeNotifier {
  WebSocketChannel? _channel;
  bool _isConnected = false;
  String _currentIp = '';
  List<AgentEvent> _events = [];
  AgentEvent? _latestWaitEvent;

  bool get isConnected => _isConnected;
  String get currentIp => _currentIp;
  List<AgentEvent> get events => _events;
  AgentEvent? get latestWaitEvent => _latestWaitEvent;

  void connect(String ip) {
    if (_isConnected) {
      disconnect();
    }
    
    _currentIp = ip;
    final wsUrl = Uri.parse('ws://$ip:9172/ws');
    
    try {
      _channel = WebSocketChannel.connect(wsUrl);
      _isConnected = true;
      notifyListeners();

      _channel!.stream.listen(
        (message) {
          try {
            final data = jsonDecode(message);
            final event = AgentEvent.fromJson(data);
            _events.insert(0, event); // Add to top

            // If it's a waiting event, we show it prominently
            if (event.action == 'tool_use_waiting' || event.action == 'AskUserQuestion') {
              _latestWaitEvent = event;
            } else if (event.action == 'tool_result_received') {
              // Clear the wait event if the action is resolved
              if (_latestWaitEvent != null) {
                _latestWaitEvent = null;
              }
            }

            notifyListeners();
          } catch (e) {
            print("AgentMonitor JSON Parse Error: $e");
          }
        },
        onDone: () {
          _isConnected = false;
          _channel = null;
          notifyListeners();
        },
        onError: (error) {
          print("AgentMonitor WS Error: $error");
          _isConnected = false;
          _channel = null;
          notifyListeners();
        },
      );
    } catch (e) {
      print("AgentMonitor Connection Error: $e");
      _isConnected = false;
      notifyListeners();
    }
  }

  void disconnect() {
    _channel?.sink.close();
    _channel = null;
    _isConnected = false;
    _events.clear();
    _latestWaitEvent = null;
    notifyListeners();
  }

  void sendAction(String action) {
    if (_channel != null && _isConnected) {
      _channel!.sink.add(jsonEncode({'action': action}));
      if (action == 'approve' || action == 'reject') {
         _latestWaitEvent = null;
         notifyListeners();
      }
    }
  }
}
