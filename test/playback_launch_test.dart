import 'dart:async';

import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/diary_service.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/playback_launch_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

class _DeferredDetailRepository extends FixtureRepository {
  final pending = <Completer<DramaDetail>>[];
  final requestIds = <String>[];
  final canceled = <String>[];

  @override
  Future<DramaDetail> playbackDetail(
    Drama drama, {
    required String requestId,
  }) {
    requestIds.add(requestId);
    final request = Completer<DramaDetail>();
    pending.add(request);
    return request.future;
  }

  @override
  Future<void> cancelPlaybackDetail(String requestId) async {
    canceled.add(requestId);
  }
}

void main() {
  setUp(DiaryService.clear);
  tearDown(DiaryService.clear);

  Future<void> mount(
    WidgetTester tester,
    _DeferredDetailRepository repository,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => FilledButton(
              onPressed: () => unawaited(
                openPlaybackDirectly(
                  context,
                  drama: FixtureRepository.free,
                  repository: repository,
                  store: store,
                ),
              ),
              child: const Text('打开剧集'),
            ),
          ),
        ),
      ),
    );
  }

  Future<void> enter(WidgetTester tester) async {
    await tester.tap(find.text('打开剧集'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
  }

  testWidgets('opening shows progress and leaving cancels its detail request', (
    tester,
  ) async {
    final repository = _DeferredDetailRepository();
    await mount(tester, repository);
    await enter(tester);
    expect(find.text('正在进入播放'), findsOneWidget);
    expect(repository.pending, hasLength(1));
    await tester.pageBack();
    await tester.pumpAndSettle();
    expect(repository.canceled, repository.requestIds);
    repository.pending.single.completeError(
      AppFailure('context canceled'),
    );
    await tester.pump();
    expect(find.byType(SnackBar), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('old errors stay silent while current timeout can be retried', (
    tester,
  ) async {
    final repository = _DeferredDetailRepository();
    await mount(tester, repository);
    await enter(tester);
    await tester.pageBack();
    await tester.pumpAndSettle();
    await enter(tester);
    expect(repository.requestIds[0], isNot(repository.requestIds[1]));
    repository.pending[0].completeError(AppFailure('旧请求错误'));
    await tester.pump();
    expect(find.text('旧请求错误'), findsNothing);
    expect(find.text('正在进入播放'), findsOneWidget);
    repository.pending[1].completeError(
      AppFailure(
        'Get https://detail.test/private/5073?token=secret: '
        'context deadline exceeded',
      ),
    );
    await tester.pump();
    expect(find.text('剧集详情加载超时，请稍后重试或检查网络。'), findsOneWidget);
    expect(find.byType(SnackBar), findsNothing);
    expect(DiaryService.fullText, contains('context deadline exceeded'));
    expect(DiaryService.fullText, isNot(contains('token=secret')));
    await tester.tap(find.text('重试'));
    await tester.pump();
    expect(repository.pending, hasLength(3));
    expect(find.text('正在进入播放'), findsOneWidget);
    repository.pending[2].complete(DramaDetail(FixtureRepository.free, []));
    await tester.pumpAndSettle();
    expect(find.text('暂时没有可播放的集数'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
