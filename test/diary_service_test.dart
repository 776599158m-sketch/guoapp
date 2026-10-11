import 'package:duanju_app/diary_service.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  setUp(DiaryService.clear);
  tearDown(DiaryService.clear);

  test('playback diagnostics preserve failures without addresses or keys', () {
    const key = '1234567890abcdef1234567890abcdef';
    DiaryService.addPlayback(
      'HTTP 403 https://user:password@media.test/private/video.mp4?token=secret '
      'decryption_key=$key key=${key.toUpperCase()}',
      decryptionKey: key,
    );
    final text = DiaryService.fullText;
    expect(text, contains('HTTP 403'));
    expect(text, contains('https://media.test/'));
    for (final secret in ['password', 'private', 'token', 'secret', key]) {
      expect(text.toLowerCase(), isNot(contains(secret)));
    }
  });

  test('playback diagnostics keep loopback ports and redact unknown keys', () {
    DiaryService.addPlayback(
      'Connection refused http://127.0.0.1:51234/session/asset.m3u8 '
      'decryption_key=abcdef1234567890abcdef1234567890',
    );
    final text = DiaryService.fullText;
    expect(text, contains('Connection refused'));
    expect(text, contains('http://127.0.0.1:51234/'));
    expect(text, isNot(contains('/session/')));
    expect(text, isNot(contains('abcdef1234567890')));
  });
}
