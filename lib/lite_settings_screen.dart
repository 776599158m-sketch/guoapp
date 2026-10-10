import 'dart:convert';
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';

import 'app_build.dart';
import 'app_layout.dart';
import 'app_theme.dart';
import 'local_store.dart';
import 'source_gate_dialog.dart';

class LiteSettingsScreen extends StatefulWidget {
  const LiteSettingsScreen({super.key, required this.store, this.recovery = false});
  final LocalStore store;
  final bool recovery;

  @override
  State<LiteSettingsScreen> createState() => _LiteSettingsScreenState();
}

class _LiteSettingsScreenState extends State<LiteSettingsScreen> {
  bool _busy = false;
  String? _message;

  Future<void> _run(Future<void> Function() action) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      await action();
    } catch (error) {
      if (mounted) setState(() => _message = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _theme() async {
    final selected = await showDialog<String>(
      context: context,
      builder: (context) => SimpleDialog(
        title: const Text('外观主题'),
        children: [
          for (final mode in ['system', 'light', 'dark'])
            SimpleDialogOption(
              onPressed: () => Navigator.pop(context, mode),
              child: Row(children: [
                Expanded(child: Text(AppTheme.label(mode))),
                if (widget.store.themeMode == mode) const Icon(Icons.check),
              ]),
            ),
        ],
      ),
    );
    if (selected != null && mounted) {
      await _run(() => widget.store.setThemeMode(selected));
    }
  }

  Future<void> _backup(bool restore) => _run(() async {
    if (!restore) {
      final content = await widget.store.exportBackup();
      final saved = await FilePicker.saveFile(
        fileName: appSlug + '-backup-' + DateTime.now().toIso8601String().substring(0, 10) + '.json',
        bytes: Uint8List.fromList(utf8.encode(content)),
        mimeType: 'application/json',
      );
      if (saved != null && mounted) setState(() => _message = '备份已保存');
      return;
    }
    final file = await FilePicker.pickFile(
      type: FileType.custom,
      allowedExtensions: ['json'],
    );
    if (file == null || !mounted) return;
    final size = await file.length();
    if (size == null || size > 8 * 1024 * 1024) {
      throw const FormatException('备份文件过大或无法读取');
    }
    final content = utf8.decode(await file.readAsBytes());
    widget.store.validateBackup(content);
    if (!mounted) return;
    final accepted = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('恢复 Lite 备份？'),
        content: const Text('将替换本机的追剧、观看记录、搜索记录和偏好设置。此操作不能撤销，请先保存当前备份。'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('取消')),
          FilledButton(onPressed: () => Navigator.pop(context, true), child: const Text('恢复')),
        ],
      ),
    );
    if (accepted != true || !mounted) return;
    if (widget.recovery) {
      await widget.store.recoverLiteBackup(content);
    } else {
      await widget.store.importBackup(content);
      if (mounted) Navigator.of(context).popUntil((route) => route.isFirst);
    }
  });

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.store,
    builder: (_, _) => Scaffold(
      appBar: AppBar(title: Text(widget.recovery ? '恢复记录' : '设置与备份')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 640),
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              if (_busy) const LinearProgressIndicator(),
              if (widget.recovery) ...[
                Text(widget.store.configurationError ?? '记录暂时无法读取'),
                const SizedBox(height: 16),
                FilledButton(
                  onPressed: _busy ? null : () => _run(widget.store.reload),
                  child: const Text('重新读取'),
                ),
              ] else ...[
                ListTile(
                  key: const ValueKey('theme-setting'),
                  leading: const Icon(Icons.palette_outlined),
                  title: const Text('外观主题'),
                  subtitle: Text(AppTheme.label(widget.store.themeMode)),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: _busy ? null : _theme,
                ),
                SwitchListTile(
                  key: const ValueKey('lite-hide-vip'),
                  secondary: const Icon(Icons.filter_alt_outlined),
                  title: const Text('隐藏 VIP 剧集'),
                  value: widget.store.hideVip,
                  onChanged: _busy ? null : (value) => _run(() => widget.store.setHideVip(value)),
                ),
                ListTile(
                  leading: const Icon(Icons.lock_outline),
                  title: const Text('站源密码锁'),
                  subtitle: Text(widget.store.sourceGateOff ? '显示全部可用站源' : '管理隐藏站源的显示与密码'),
                  onTap: _busy ? null : () => showSourceGateDialog(context, widget.store),
                ),
                const Divider(),
                ListTile(
                  key: const ValueKey('lite-export-backup'),
                  leading: const Icon(Icons.save_alt),
                  title: const Text('保存备份'),
                  subtitle: const Text('将追剧和观看记录保存到文件'),
                  onTap: _busy ? null : () => _backup(false),
                ),
              ],
              ListTile(
                key: const ValueKey('lite-import-backup'),
                leading: const Icon(Icons.restore),
                title: const Text('恢复备份'),
                subtitle: const Text('选择本应用保存的 iOS Lite 备份'),
                onTap: _busy ? null : () => _backup(true),
              ),
              if (_message != null)
                Padding(padding: const EdgeInsets.all(16), child: Text(_message!)),
              if (!widget.recovery) ...[
                const Divider(),
                ListTile(
                  title: const Text(appName),
                  subtitle: const Text(appVersion),
                  leading: const Icon(Icons.play_circle_outline),
                ),
              ],
            ],
          ),
        ),
      ),
    ),
  );
}
