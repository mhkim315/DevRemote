import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import 'package:get/get.dart';
import '../../models/agent_monitor.dart';
import 'home_page.dart';

class AgentMonitorPage extends PageShape {
  @override
  final String title = "AI Vibe Monitor";
  
  @override
  final Widget icon = const Icon(Icons.rocket_launch);

  @override
  final List<Widget> appBarActions = [];

  @override
  Widget build(BuildContext context) {
    return ChangeNotifierProvider(
      create: (_) => AgentMonitorModel(),
      child: const _AgentMonitorView(),
    );
  }
}

class _AgentMonitorView extends StatefulWidget {
  const _AgentMonitorView({Key? key}) : super(key: key);

  @override
  _AgentMonitorViewState createState() => _AgentMonitorViewState();
}

class _AgentMonitorViewState extends State<_AgentMonitorView> {
  final TextEditingController _ipController = TextEditingController(text: '192.168.');

  @override
  void dispose() {
    _ipController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final model = context.watch<AgentMonitorModel>();

    return Stack(
      children: [
        Scaffold(
          body: Padding(
            padding: const EdgeInsets.all(16.0),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  'Connect to Vibe Daemon',
                  style: TextStyle(fontSize: 20, fontWeight: FontWeight.bold),
                ),
                const SizedBox(height: 16),
                Row(
                  children: [
                    Expanded(
                      child: TextField(
                        controller: _ipController,
                        decoration: const InputDecoration(
                          labelText: 'Daemon Local IP',
                          hintText: 'e.g. 192.168.0.100',
                          border: OutlineInputBorder(),
                        ),
                      ),
                    ),
                    const SizedBox(width: 16),
                    ElevatedButton(
                      onPressed: () {
                        if (model.isConnected) {
                          model.disconnect();
                        } else {
                          model.connect(_ipController.text);
                        }
                      },
                      style: ElevatedButton.styleFrom(
                        backgroundColor: model.isConnected ? Colors.red : Colors.blue,
                        padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 16),
                      ),
                      child: Text(model.isConnected ? 'Disconnect' : 'Connect'),
                    ),
                  ],
                ),
                const SizedBox(height: 24),
                const Text(
                  'Event Log',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
                ),
                const SizedBox(height: 8),
                Expanded(
                  child: Container(
                    decoration: BoxDecoration(
                      border: Border.all(color: Colors.grey.shade300),
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: ListView.builder(
                      itemCount: model.events.length,
                      itemBuilder: (context, index) {
                        final event = model.events[index];
                        return ListTile(
                          leading: Icon(
                            event.action.contains('waiting') ? Icons.hourglass_top : Icons.check_circle,
                            color: event.action.contains('waiting') ? Colors.orange : Colors.green,
                          ),
                          title: Text(event.action),
                          subtitle: Text(event.command.isNotEmpty ? event.command : event.message),
                        );
                      },
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),

        // Vibe Event Card Overlay
        if (model.latestWaitEvent != null)
          Container(
            color: Colors.black54, // Dim background
            child: Center(
              child: Card(
                margin: const EdgeInsets.symmetric(horizontal: 24),
                elevation: 12,
                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
                child: Padding(
                  padding: const EdgeInsets.all(24.0),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(Icons.warning_amber_rounded, size: 64, color: Colors.orange),
                      const SizedBox(height: 16),
                      const Text(
                        'Approval Required',
                        style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 16),
                      Container(
                        padding: const EdgeInsets.all(12),
                        decoration: BoxDecoration(
                          color: Colors.grey.shade100,
                          borderRadius: BorderRadius.circular(8),
                          border: Border.all(color: Colors.grey.shade300),
                        ),
                        child: Text(
                          model.latestWaitEvent!.command,
                          style: const TextStyle(fontFamily: 'monospace', fontSize: 14),
                        ),
                      ),
                      const SizedBox(height: 24),
                      Row(
                        mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                        children: [
                          OutlinedButton.icon(
                            onPressed: () => model.sendAction('reject'),
                            icon: const Icon(Icons.close, color: Colors.red),
                            label: const Text('Reject (N)', style: TextStyle(color: Colors.red)),
                            style: OutlinedButton.styleFrom(
                              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
                            ),
                          ),
                          ElevatedButton.icon(
                            onPressed: () => model.sendAction('approve'),
                            icon: const Icon(Icons.check),
                            label: const Text('Approve (Y)'),
                            style: ElevatedButton.styleFrom(
                              backgroundColor: Colors.green,
                              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
      ],
    );
  }
}
