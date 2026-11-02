import assert from "node:assert/strict";
import { test } from "node:test";
import { taskBillingDisplayAmount } from "./generation-task-display";

test("task billing shows actual charges, released funds, and original estimates by status", () => {
    const billing = { amountMicrocredits: 33335, reservedMicrocredits: 33335, chargedMicrocredits: 853376, refundedMicrocredits: 12345 };
    assert.equal(taskBillingDisplayAmount({ ...billing, status: "settled" }), 853376);
    assert.equal(taskBillingDisplayAmount({ ...billing, chargedMicrocredits: 0, status: "settled" }), 0);
    assert.equal(taskBillingDisplayAmount({ ...billing, status: "refunded" }), 12345);
    for (const status of ["reserved", "running", "uncertain"] as const) {
        assert.equal(taskBillingDisplayAmount({ ...billing, status }), 33335);
    }
});
