import 'dart:async';
import 'dart:convert';

import 'package:duanju_app/app_build.dart';
import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/diary_service.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/main.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/ranking_models.dart';
import 'package:duanju_app/rankings_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

void main() {
  Future<LocalStore> freshStore() async {
    SharedPreferences.setMockInitialValues({});
    return LocalStore(await SharedPreferences.getInstance());
  }

  testWidgets('leaving rankings cancels its request and ignores a late error', (
    tester,
  ) async {
    final repository = _DeferredRankingRepository();
    SharedPreferences.setMockInitialValues({'sourceGateOff': true});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await tester.pumpWidget(MaterialApp(home: RankingsScreen(
      repository: repository,
      store: store,
      initialGroup: 'huangguo',
    )));
    await tester.pump();
    expect(repository.pending, hasLength(1));
    await tester.pumpWidget(const SizedBox.shrink());
    expect(repository.canceled, repository.requestIds);
    repository.pending.single.completeError(AppFailure('context canceled'));
    await tester.pump();
    expect(tester.takeException(), isNull);
  });

  testWidgets('ranking network errors hide addresses and retry in the same scope', (
    tester,
  ) async {
    DiaryService.clear();
    addTearDown(DiaryService.clear);
    final repository = _DeferredRankingRepository();
    SharedPreferences.setMockInitialValues({'sourceGateOff': true});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await tester.pumpWidget(MaterialApp(home: RankingsScreen(
      repository: repository,
      store: store,
      initialGroup: 'huangguo',
    )));
    await tester.pump();
    repository.pending.single.completeError(AppFailure(
      'Get https://ranking.test/private?token=secret: '
      'read tcp 192.168.1.2:50000: connection reset by peer',
    ));
    await tester.pumpAndSettle();
    expect(find.text('连接站源失败，请检查网络或代理后重试。'), findsOneWidget);
    expect(find.textContaining('192.168.1.2'), findsNothing);
    expect(find.textContaining('ranking.test'), findsNothing);
    expect(DiaryService.fullText, contains('connection reset by peer'));
    expect(DiaryService.fullText, isNot(contains('token=secret')));
    await tester.tap(find.text('重试'));
    await tester.pump();
    expect(repository.pending, hasLength(2));
    expect(repository.requestIds[0], repository.requestIds[1]);
    repository.pending.last.complete(const RankingPage(items: []));
    await tester.pumpAndSettle();
    expect(find.text('连接站源失败，请检查网络或代理后重试。'), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
    expect(tester.takeException(), isNull);
  });

  test('interactive errors preserve business messages and summarize network errors', () {
    expect(interactiveFailureMessage(AppFailure('没有可播放的集数'), fallback: '加载失败'), '没有可播放的集数');
    expect(interactiveFailureMessage(TimeoutException('private'), fallback: '加载失败'), '连接等待超时，请检查网络或代理后重试。');
    expect(interactiveFailureMessage(AppFailure('Get https://test/private: EOF'), fallback: '加载失败'), '加载失败');
  });

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

  test('Damaged local records can recover from a valid Lite backup', () async {
    final source = await freshStore();
    addTearDown(source.dispose);
    await source.toggleFavorite(FixtureRepository.free);
    final backup = await source.exportBackup();
    SharedPreferences.setMockInitialValues({'profiles': 'broken'});
    final recovered = LocalStore(await SharedPreferences.getInstance());
    addTearDown(recovered.dispose);
    expect(recovered.locked, isTrue);
    expect(recovered.configurationError, isNotNull);
    await recovered.recoverLiteBackup(backup);
    expect(recovered.locked, isFalse);
    expect(recovered.isFavorite(FixtureRepository.free.id), isTrue);
    final reopened = LocalStore(recovered.preferences);
    addTearDown(reopened.dispose);
    expect(reopened.locked, isFalse);
    expect(reopened.isFavorite(FixtureRepository.free.id), isTrue);
    await expectLater(reopened.saveProfile(
      name: 'extra',
      sources: ['hongguo'],
      download: true,
    ), throwsStateError);
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

class _DeferredRankingRepository extends FixtureRepository {
  final pending = <Completer<RankingPage>>[];
  final requestIds = <String>[];
  final canceled = <String>[];

  @override
  Future<List<RankingBoard>> rankingBoards() async => const [
    RankingBoard(id: 'huangguo-hot', source: 'huangguoai', name: '热播榜'),
  ];

  @override
  Future<RankingPage> rankingRead(String board, {
    required String requestId,
    int page = 1,
    bool force = false,
  }) {
    requestIds.add(requestId);
    final result = Completer<RankingPage>();
    pending.add(result);
    return result.future;
  }

  @override
  Future<void> cancelRankingRead(String requestId) async {
    canceled.add(requestId);
  }
}
