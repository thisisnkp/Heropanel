import { beforeEach, describe, expect, it } from "vitest";
import { createPinia, setActivePinia } from "pinia";

import { useFileManagerStore } from "./fileManager";

describe("file manager store", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("opens on the document root, not the account root", () => {
    const fm = useFileManagerStore();
    // The account root holds DO_NOT_UPLOAD_HERE; landing there by default would
    // invite exactly the mistake that file warns about.
    expect(fm.path).toEqual(["public_html"]);
    expect(fm.rows.map((r) => r.name)).toContain("wp-config.php");
  });

  it("walks into a folder and back out through the breadcrumb", () => {
    const fm = useFileManagerStore();
    fm.enter("wp-content");
    expect(fm.pathKey).toBe("public_html/wp-content");
    expect(fm.rows.map((r) => r.name)).toEqual(["plugins", "themes", "uploads", "index.php"]);

    fm.goTo(0);
    expect(fm.pathKey).toBe("public_html");
    fm.goTo(-1);
    expect(fm.pathKey).toBe("");
    expect(fm.rows.map((r) => r.name)).toContain("DO_NOT_UPLOAD_HERE");
  });

  describe("search", () => {
    it("filters within the folder when it can", () => {
      const fm = useFileManagerStore();
      fm.query = "wp-";
      const names = fm.rows.map((r) => r.name);
      expect(names).toContain("wp-admin");
      expect(names).toContain("wp-config.php");
      expect(names.every((n) => n.includes("wp-"))).toBe(true);
      // A local hit is not tagged with a location — you are already there.
      expect(fm.rows.every((r) => r.where === undefined)).toBe(true);
    });

    it("falls back to the whole account rather than saying no results", () => {
      const fm = useFileManagerStore();
      fm.query = "themes";
      // `themes` lives one level down; a file manager that reports "no results"
      // while the file exists is technically correct and useless.
      expect(fm.rows).toHaveLength(1);
      expect(fm.rows[0].name).toBe("themes");
      expect(fm.rows[0].where).toBe("public_html/wp-content");
      expect(fm.rows[0].tag).toBe("in public_html/wp-content");
    });

    it("reports an honest miss when nothing matches anywhere", () => {
      const fm = useFileManagerStore();
      fm.query = "no-such-file-anywhere";
      expect(fm.rows).toHaveLength(0);
      expect(fm.isEmpty).toBe(true);
    });

    it("is cleared by navigating, so the next folder is not pre-filtered", () => {
      const fm = useFileManagerStore();
      fm.query = "wp-";
      fm.enter("wp-admin");
      expect(fm.query).toBe("");
    });
  });

  describe("selection", () => {
    it("replaces the selection on a plain click and adds on multi-select", () => {
      const fm = useFileManagerStore();
      fm.selectOne("index.php");
      fm.selectOne("robots.txt");
      expect(fm.selected).toEqual(["robots.txt"]);

      fm.toggleMultiSelect();
      expect(fm.selected).toEqual([]);
      fm.selectOne("index.php");
      fm.selectOne("robots.txt");
      expect(fm.selected).toEqual(["index.php", "robots.txt"]);
    });

    it("clears the selection when the view changes", () => {
      const fm = useFileManagerStore();
      fm.selectOne("index.php");
      fm.setView("trash");
      expect(fm.selected).toEqual([]);
    });
  });

  describe("trash", () => {
    it("moves a file out of its folder and into the trash", () => {
      const fm = useFileManagerStore();
      const before = fm.rows.length;

      fm.moveToTrash(["robots.txt"]);
      expect(fm.rows).toHaveLength(before - 1);
      expect(fm.rows.map((r) => r.name)).not.toContain("robots.txt");
      expect(fm.trash.map((t) => t.name)).toContain("robots.txt");
      expect(fm.selected).toEqual([]);
    });

    it("restores to the folder it came from, not the folder that is open", () => {
      const fm = useFileManagerStore();
      fm.moveToTrash(["robots.txt"]);

      // Wander somewhere else before restoring.
      fm.enter("wp-content");
      fm.restore("robots.txt");

      expect(fm.trash.map((t) => t.name)).not.toContain("robots.txt");
      expect(fm.rows.map((r) => r.name)).not.toContain("robots.txt");

      fm.goTo(0);
      expect(fm.rows.map((r) => r.name)).toContain("robots.txt");
    });

    it("restores an item that was in the trash before this session", () => {
      const fm = useFileManagerStore();
      // The fixture ships one pre-existing trashed file. Restoring it must put
      // it back rather than delete it from both places.
      expect(fm.trash.map((t) => t.name)).toContain("old-theme-backup.zip");
      fm.restore("old-theme-backup.zip");
      expect(fm.trash.map((t) => t.name)).not.toContain("old-theme-backup.zip");
      expect(fm.rows.map((r) => r.name)).toContain("old-theme-backup.zip");
    });

    it("shows the trash contents when the trash view is open", () => {
      const fm = useFileManagerStore();
      fm.setView("trash");
      expect(fm.rows).toEqual(fm.trash);
    });
  });

  describe("create", () => {
    it("adds into the folder that is open", () => {
      const fm = useFileManagerStore();
      fm.enter("wp-content");
      fm.create("dir", "cache");
      expect(fm.rows.map((r) => r.name)).toContain("cache");

      fm.goTo(0);
      expect(fm.rows.map((r) => r.name)).not.toContain("cache");
    });

    it("marks a new folder and a new file differently", () => {
      const fm = useFileManagerStore();
      fm.create("dir", "assets2");
      fm.create("file", "notes.txt");
      const rows = Object.fromEntries(fm.rows.map((r) => [r.name, r]));
      expect(rows["assets2"].type).toBe("dir");
      expect(rows["assets2"].perm).toBe("0755");
      expect(rows["notes.txt"].type).toBe("file");
      expect(rows["notes.txt"].perm).toBe("0644");
    });
  });

  describe("editor tabs", () => {
    it("opens a file once, however many times it is opened", () => {
      const fm = useFileManagerStore();
      fm.openInEditor("index.php");
      fm.openInEditor("wp-config.php");
      fm.openInEditor("index.php");
      expect(fm.tabs).toEqual(["index.php", "wp-config.php"]);
      expect(fm.openFile).toBe("index.php");
    });

    it("falls back to the remaining tab when the open one is closed", () => {
      const fm = useFileManagerStore();
      fm.openInEditor("index.php");
      fm.openInEditor("wp-config.php");
      fm.closeTab("wp-config.php");
      expect(fm.openFile).toBe("index.php");

      fm.closeTab("index.php");
      expect(fm.openFile).toBeNull();
    });
  });
});
