const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const ui = require("./dist/product_downloadable_section.js");

describe("product_downloadable_section", () => {
  it("renders create note before save", () => {
    const html = ui.renderDownloadableSectionHTML(false);
    assert.match(html, /Save the product as downloadable/);
  });

  it("renders files table and settings form for edit", () => {
    const section = ui.renderDownloadableSectionHTML(true);
    assert.match(section, /data-downloadable-files/);
    assert.match(section, /data-downloadable-settings/);
    assert.match(section, /global_retarget/);
    assert.match(section, /not product-scoped/);
    const table = ui.filesTableHTML([{ asset_id: "a1", filename: "doc.pdf", position: 1, label: "Manual" }]);
    assert.match(table, /doc\.pdf/);
    assert.match(table, /data-attach-file-form/);
    assert.match(table, /private_download/);
    const settings = ui.settingsFormHTML({ expiry_days: 30, download_limit: 5 });
    assert.match(settings, /value="30"/);
    assert.match(settings, /data-clear-grant-settings/);
  });
});
