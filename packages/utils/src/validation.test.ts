/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { describe, expect, it } from "vitest";

import {
  hasInjectionRiskChars,
  validateCompanyName,
  validateDisplayName,
  validatePersonName,
  validateSlug,
  validateWorkspaceName,
} from "./validation";

describe("validatePersonName", () => {
  it("accepts a name", () => {
    for (const name of ["Ada Lovelace", "李雷", "Jean-Luc", "Ада"]) {
      expect(validatePersonName(name), name).toBe(true);
    }
  });

  // This is what the change to this function was about. The allowlist regex permits an apostrophe
  // and the failure message promises one, but a blocklist ran first and rejected it, so a large
  // number of real surnames could not be entered at all.
  it("accepts an apostrophe, which a great many surnames contain", () => {
    for (const name of ["O'Neill", "D'Angelo", "N'Diaye"]) {
      expect(validatePersonName(name), name).toBe(true);
    }
  });

  it("still refuses everything the allowlist does not name", () => {
    for (const name of [
      "<script>alert(1)</script>",
      'say "hi"',
      "a{b}",
      "a[b]",
      "a*b",
      "a^b",
      "a!b",
      "a#b",
      "a%b",
      "Bob123",
    ]) {
      expect(validatePersonName(name), name).toBe("Names can only contain letters, spaces, hyphens, and apostrophes");
    }
  });

  it("asks for something rather than nothing", () => {
    expect(validatePersonName("")).toBe("Name is required");
    expect(validatePersonName("   ")).toBe("Name is required");
  });

  it("has a length limit", () => {
    expect(validatePersonName("a".repeat(50))).toBe(true);
    expect(validatePersonName("a".repeat(51))).toBe("Name must be 50 characters or less");
  });
});

describe("validateDisplayName", () => {
  it("accepts letters, numbers, periods, hyphens and underscores", () => {
    for (const name of ["ada_1", "ada.lovelace", "ada-lovelace", "李雷"]) {
      expect(validateDisplayName(name), name).toBe(true);
    }
  });

  it("refuses a space", () => {
    expect(validateDisplayName("has space")).toBe(
      "Display name can only contain letters, numbers, periods, hyphens, and underscores"
    );
  });

  // Recorded rather than endorsed: an empty display name is accepted here, and whether one is
  // required is decided by the caller.
  it("treats an empty one as somebody else's decision", () => {
    expect(validateDisplayName("")).toBe(true);
  });
});

describe("validateSlug", () => {
  it("accepts what a url path segment can carry", () => {
    for (const slug of ["my-team", "my_team", "team1", "UPPER"]) {
      expect(validateSlug(slug), slug).toBe(true);
    }
  });

  it("refuses a space and demands a value", () => {
    expect(validateSlug("has space")).toBe("Slug can only contain letters, numbers, hyphens, and underscores");
    expect(validateSlug("")).toBe("Slug is required");
  });
});

describe("validateWorkspaceName and validateCompanyName", () => {
  it("accept an ordinary name", () => {
    expect(validateWorkspaceName("Acme Inc")).toBe(true);
    expect(validateCompanyName("Acme Inc")).toBe(true);
  });

  it("refuse the characters a blocklist is there for", () => {
    expect(validateWorkspaceName("<script>")).toBe(
      "Workspace name cannot contain special characters like < > ' \" { } [ ] * ^ ! # %"
    );
  });

  // Both take a `required` flag, and neither asks for a value unless it is set.
  it("only demand a value when asked to", () => {
    expect(validateWorkspaceName("")).toBe(true);
    expect(validateCompanyName("")).toBe(true);
    expect(validateWorkspaceName("", true)).not.toBe(true);
    expect(validateCompanyName("", true)).not.toBe(true);
  });
});

describe("hasInjectionRiskChars", () => {
  it("spots the characters it names", () => {
    for (const input of ["<script>", "a'b", 'a"b', "a{b", "a}b", "a[b", "a]b", "a*b", "a^b", "a!b", "a#b", "a%b"]) {
      expect(hasInjectionRiskChars(input), input).toBe(true);
    }
  });

  it("leaves ordinary text alone", () => {
    for (const input of ["plain", "Acme Inc", "", "李雷"]) {
      expect(hasInjectionRiskChars(input), input).toBe(false);
    }
  });

  // Worth knowing: this is about markup and quoting, not about SQL. A semicolon or a comment marker
  // passes, and nothing here should be relied on as a database defence.
  it("is not a SQL filter", () => {
    expect(hasInjectionRiskChars("a;b")).toBe(false);
    expect(hasInjectionRiskChars("a--b")).toBe(false);
  });
});
