import assert from "node:assert/strict";
import { test } from "node:test";
import { formatVideoResolutionLabel, normalizeVideoResolution, videoDimensionsForRatioAndResolution, videoResolutionComparisonKey } from "./video-generation-options";

test("workflow megapixel tiers survive selection without becoming pixel dimensions", () => {
    for (const value of ["0.5", "0.9", "1.5", "2.0"]) {
        assert.equal(normalizeVideoResolution(value), value);
        assert.equal(formatVideoResolutionLabel(value), `${value} MP`);
        assert.equal(videoResolutionComparisonKey(value), value);
        assert.equal(videoDimensionsForRatioAndResolution("16:9", value), undefined);
    }
});

test("pixel presets and opaque channel enums keep their existing meaning", () => {
    for (const [value, normalized, label] of [["480p", "480", "480P"], ["720", "720", "720P"], ["2K", "1440", "2K"], ["4k", "2160", "4K"], ["768p竖", "768p竖", "768P竖"]]) {
        assert.equal(normalizeVideoResolution(value), normalized);
        assert.equal(formatVideoResolutionLabel(value), label);
    }
    assert.deepEqual(videoDimensionsForRatioAndResolution("16:9", "720p"), { width: 1280, height: 720 });
    assert.deepEqual(videoDimensionsForRatioAndResolution("9:16", "768p竖"), { width: 768, height: 1366 });
});
