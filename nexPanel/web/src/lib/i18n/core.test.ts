import { describe, expect, it } from "vitest";
import { formatMessage, translate, type Catalog } from "./core";

describe("formatMessage", () => {
  it("substitutes placeholders from vars", () => {
    expect(formatMessage("Hi {{name}}", { name: "Ada" })).toBe("Hi Ada");
    expect(formatMessage("{{a}} and {{b}}", { a: 1, b: 2 })).toBe("1 and 2");
  });

  it("tolerates whitespace inside the braces", () => {
    expect(formatMessage("Hi {{ name }}", { name: "Ada" })).toBe("Hi Ada");
  });

  it("leaves an unknown placeholder intact so a missing var is visible", () => {
    expect(formatMessage("Hi {{name}}", {})).toBe("Hi {{name}}");
    expect(formatMessage("Hi {{name}}")).toBe("Hi {{name}}");
  });
});

describe("translate", () => {
  const en: Catalog = {
    "greeting": "Hello",
    "welcome": "Welcome, {{name}}",
    "items": { one: "{{count}} item", other: "{{count}} items" },
  };
  const es: Catalog = {
    "greeting": "Hola",
    // "welcome" and "items" intentionally untranslated → must fall back to en.
  };

  it("uses the active catalog when it has the key", () => {
    expect(translate(es, en, "greeting")).toBe("Hola");
  });

  it("falls back to the base catalog for an untranslated key", () => {
    expect(translate(es, en, "welcome", { name: "Ada" })).toBe("Welcome, Ada");
  });

  it("falls back to the key itself when nobody has it", () => {
    expect(translate(es, en, "nope.missing")).toBe("nope.missing");
  });

  it("selects the plural form and exposes count to placeholders", () => {
    expect(translate(en, en, "items", undefined, 1)).toBe("1 item");
    expect(translate(en, en, "items", undefined, 5)).toBe("5 items");
    expect(translate(es, en, "items", undefined, 3)).toBe("3 items"); // via fallback
  });
});
