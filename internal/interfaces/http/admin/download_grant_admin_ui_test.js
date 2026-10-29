const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const ui = require("./dist/download_grant_admin_ui.js");

describe("download_grant_admin_ui", () => {
  it("renders grant rows with revoke/reissue when writable", () => {
    const html = ui.grantsTableHTML([{
      id: "g1",
      order_item_id: "o/v",
      asset_id: "a1",
      download_count: 1,
      download_limit: 5,
      expires_at: "2030-01-01T00:00:00Z",
      expired: false,
      exhausted: false
    }], true);
    assert.match(html, /data-revoke-grant="g1"/);
    assert.match(html, /data-reissue-grant="g1"/);
    assert.match(html, /live/);
  });

  it("omits write actions without permission", () => {
    const html = ui.grantsTableHTML([{ id: "g1", expired: true, exhausted: false }], false);
    assert.doesNotMatch(html, /data-revoke-grant/);
    assert.match(html, /expired/);
  });
});
