import { expect, test } from "bun:test";

import { workbenchTaskNeedsAssetMaterialization } from "../src/pages/workbench/workbench-batches";

test("工作台只登记带有结果且仍缺资产关联的成功任务", () => {
    expect(workbenchTaskNeedsAssetMaterialization({ status: "running", resultJson: "{}" })).toBe(false);
    expect(workbenchTaskNeedsAssetMaterialization({ status: "succeeded" })).toBe(false);
    expect(workbenchTaskNeedsAssetMaterialization({ status: "succeeded", resultJson: "{}" })).toBe(true);
    expect(workbenchTaskNeedsAssetMaterialization({ status: "succeeded", outputs: [{ outputIndex: 0, mediaType: "video", materializedAssetId: "asset-1" }] })).toBe(false);
    expect(workbenchTaskNeedsAssetMaterialization({ status: "succeeded", resultJson: "{}", outputs: [{ outputIndex: 0, mediaType: "video" }] })).toBe(true);
});
