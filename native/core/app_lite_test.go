package core

import (
    "errors"
    "testing"
)

func TestNativeLiteRejectsUnavailableActions(t *testing.T) {
    if buildLite != "true" { t.Skip("requires Lite linker flag") }
    for _, action := range []string{"lan", "downloads", "enqueueDownloads", "controlDownloads", "controlDownloadBatch", "downloadDirectory", "moveDownloads", "storage", "workLease", "localPlayback", "prepareHandoff"} {
        _, err := nativeDispatch(nativeInput{Action: action})
        if !errors.Is(err, errNativeLiteFeature) { t.Errorf("%s: %v", action, err) }
    }
    if err := nativeAuthorizeInput(nativeInput{Action: "catalog", Source: "hongguo"}); err != nil { t.Fatal(err) }
}

func TestNativeLiteEngineHasNoBackgroundServices(t *testing.T) {
    if buildLite != "true" { t.Skip("requires Lite linker flag") }
    engine, err := newNativeEngine(t.TempDir())
    if err != nil { t.Fatal(err) }
    if engine.downloads != nil || engine.lan != nil { t.Fatal("Lite started a background service") }
}

func TestNativeLiteSourceBoundary(t *testing.T) {
    if buildLite != "true" { t.Skip("requires Lite linker flag") }
    for _, source := range []string{"hanxiaoquan", "guipian", "sorani"} {
        if nativeSourceAvailable(source) { t.Errorf("unexpected provider: %s", source) }
    }
    for _, source := range []string{"hongguo", "huangju", "yeguo", "dsd", "huangdou", "huangguo-video", "huangguoai", "cloudfront"} {
        if !nativeSourceAvailable(source) { t.Errorf("missing provider: %s", source) }
    }
}
