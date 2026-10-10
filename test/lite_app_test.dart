import 'dart:convert';

import 'package:duanju_app/app_build.dart';
import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/main.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

void main() {
  Future<LocalStore> freshStore() async {
    SharedPreferences.setMockInitialValues({});
    return LocalStore(await SharedPreferences.getInstance());
  }

  test('Lite backup restores favorites, watch position and theme atomically', () async {
    expect(liteEnabled, isTrue);
    final store = await freshStore();
    addTearDown(store.dispose);
    await store.toggleFavorite(FixtureRepository.free);
    await store.saveWatch(WatchEntry(
      drama: FixtureRepository.free,
      episode: 2,
      position: 18.5,
      duration: 90,
      updatedAt: DateTime(2026, 10, 10),
    ));
    await store.setThemeMode('dark');
    final backup = await store.exportBackup();
    expect(jsonDecode(backup)['edition'], 'ios-lite');
    await store.clearHistory();
    await store.toggleFavorite(FixtureRepository.free);
    await store.setThemeMode('light');
    await store.importBackup(backup);
    expect(store.isFavorite(FixtureRepository.free.id), isTrue);
    expect(store.watched(FixtureRepository.free.id)?.episode, 2);
    expect(store.watched(FixtureRepository.free.id)?.position, 18.5);
    expect(store.themeMode, 'dark');
    expect(store.profiles, hasLength(1));
    expect(store.forceLogin, isFalse);
    expect(store.canDownload, isFalse);
    expect(NativeRepository().supportsDownloads, isFalse);
  });

  test('Full edition and malformed backups leave Lite records intact', () async {
    final store = await freshStore();
    addTearDown(store.dispose);
    await store.toggleFavorite(FixtureRepository.free);
    final valid = await store.exportBackup();
    final full = jsonDecode(valid) as Map<String, dynamic>;
    full.remove('edition');
    await expectLater(store.importBackup(jsonEncode(full)), throwsFormatException);
    final malformed = jsonDecode(valid) as Map<String, dynamic>;
    malformed['libraries']['default']['history'] = ['broken'];
    await expectLater(store.importBackup(jsonEncode(malformed)), throwsA(anything));
    expect(store.isFavorite(FixtureRepository.free.id), isTrue);
    expect(await store.exportBackup(), valid);
  });

  test('Lite source boundary excludes unsupported full-edition providers', () {
    expect(allSourcesEnabled, isTrue);
    expect(SourceSite.isAvailable('hongguo'), isTrue);
    expect(SourceSite.isAvailable('huangju'), isTrue);
    for (final source in ['sorani', 'guipian', 'hanxiaoquan']) {
      expect(SourceSite.isAvailable(source), isFalse);
    }
  });

  for (final size in [const Size(390, 844), const Size(1024, 768)]) {
    testWidgets('Lite browsing and settings at ' + size.toString(), (tester) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final store = await freshStore();
      addTearDown(store.dispose);
      await tester.pumpWidget(DuanjuApp(repository: FixtureRepository(), store: store));
      await tester.pumpAndSettle();
      expect(find.text('测试短剧'), findsOneWidget);
      expect(find.text('下载'), findsNothing);
      await tester.tap(find.byTooltip('更多'));
      await tester.pumpAndSettle();
      expect(find.text('用户管理'), findsNothing);
      expect(find.text('界面模式'), findsNothing);
      await tester.tap(find.text('设置与备份'));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('lite-export-backup')), findsOneWidget);
      expect(find.byKey(const ValueKey('lite-import-backup')), findsOneWidget);
      expect(find.text('设备互联'), findsNothing);
      expect(find.text('下载偏好'), findsNothing);
      await tester.tap(find.byKey(const ValueKey('theme-setting')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('深色'));
      await tester.pumpAndSettle();
      expect(store.themeMode, 'dark');
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    });
  }
}
