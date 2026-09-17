/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { describe, expect, it } from "vitest";

import {
  ensureTrailingSlash,
  extractHostname,
  extractTLD,
  formatURLForDisplay,
  isLocalhost,
  isValidIPv4,
  isValidIPv6,
  isValidNextPath,
  validateIPAddress,
} from "./url";

// isValidNextPath decides whether a ?next_path= is safe to send someone to after they sign in, so
// what it rejects is the whole point: anything that could take them off this origin.
describe("isValidNextPath", () => {
  it("accepts a path on this origin", () => {
    for (const path of ["/dashboard", "/ok?x=1#f", "/a/b/c/", "/"]) {
      expect(isValidNextPath(path), path).toBe(true);
    }
  });

  it("refuses anything that could leave this origin", () => {
    for (const path of [
      "//evil.example", // protocol-relative, the classic open redirect
      "http://evil.example",
      "https://evil.example",
      "dashboard", // no leading slash, so the browser would resolve it against the current page
      "",
      "   ",
      "/a\\b", // a backslash is a path separator to some parsers and not others
      "\\\\evil.example",
    ]) {
      expect(isValidNextPath(path), path).toBe(false);
    }
  });

  it("refuses a scheme smuggled into the path", () => {
    for (const path of ["/javascript:alert(1)", "/data:text/html,x", "/vbscript:x"]) {
      expect(isValidNextPath(path), path).toBe(false);
    }
  });

  it("trims before deciding", () => {
    expect(isValidNextPath("  /dashboard  ")).toBe(true);
    expect(isValidNextPath("  http://evil.example  ")).toBe(false);
  });

  // Recorded rather than endorsed: the URL constructor resolves these against the dummy base, so
  // they arrive as /etc/passwd and /b -- still paths on this origin, which is what the check is for.
  it("lets a relative segment normalise away", () => {
    expect(isValidNextPath("/../etc/passwd")).toBe(true);
    expect(isValidNextPath("/a/../b")).toBe(true);
  });
});

describe("ensureTrailingSlash", () => {
  it("adds one to a path that has none", () => {
    expect(ensureTrailingSlash("/projects")).toBe("/projects/");
    expect(ensureTrailingSlash("/a/b?x=1#f")).toBe("/a/b/?x=1#f");
  });

  it("leaves a path that already has one", () => {
    expect(ensureTrailingSlash("/projects/")).toBe("/projects/");
  });

  it("leaves the root alone", () => {
    expect(ensureTrailingSlash("/")).toBe("/");
  });

  it("keeps an absolute url absolute", () => {
    expect(ensureTrailingSlash("https://pace.example/x")).toBe("https://pace.example/x/");
    expect(ensureTrailingSlash("https://pace.example/")).toBe("https://pace.example/");
  });

  it("returns the input when it cannot be parsed", () => {
    expect(ensureTrailingSlash("")).toBe("");
  });
});

describe("extractHostname", () => {
  it("strips the scheme, credentials, port, path, query and fragment", () => {
    expect(extractHostname("https://user:pass@a.b.example:8080/x?y=1#z")).toBe("a.b.example");
  });

  it("handles a bare host and port", () => {
    expect(extractHostname("localhost:3000")).toBe("localhost");
  });
});

describe("isLocalhost", () => {
  it("knows the three spellings", () => {
    for (const url of ["http://localhost:3000", "http://127.0.0.1/x", "http://0.0.0.0"]) {
      expect(isLocalhost(url), url).toBe(true);
    }
  });

  it("says no to a real host", () => {
    expect(isLocalhost("https://pace.example")).toBe(false);
  });
});

describe("validateIPAddress", () => {
  it("recognises IPv4", () => {
    expect(validateIPAddress("192.168.1.1")).toEqual({ isValid: true, type: "ipv4", formatted: "192.168.1.1" });
  });

  it("recognises IPv6 and unwraps the brackets a url puts round it", () => {
    expect(validateIPAddress("[::1]")).toEqual({ isValid: true, type: "ipv6", formatted: "::1" });
  });

  it("rejects an octet out of range", () => {
    expect(validateIPAddress("999.1.1.1")).toEqual({ isValid: false, type: "invalid" });
    expect(isValidIPv4("256.0.0.1")).toBe(false);
    expect(isValidIPv4("192.168.1")).toBe(false);
  });

  it("rejects what is not an address at all", () => {
    expect(validateIPAddress("")).toEqual({ isValid: false, type: "invalid" });
    expect(isValidIPv6("not an address")).toBe(false);
  });
});

describe("extractTLD", () => {
  it("returns a known suffix", () => {
    expect(extractTLD("https://a.example.com/x")).toBe("com");
    expect(extractTLD("a.co.uk")).toBe("uk");
  });

  it("returns nothing when there is no known suffix to find", () => {
    for (const input of ["nope", "a.invalidtld", ".com", "example.", ""]) {
      expect(extractTLD(input), input).toBe("");
    }
  });
});

describe("formatURLForDisplay", () => {
  it("shows the host of a url", () => {
    expect(formatURLForDisplay("https://pace.example/x?y=1")).toBe("pace.example");
  });

  it("falls back to whatever it can pull out of a non-url", () => {
    expect(formatURLForDisplay("not a url")).toBe("not a url");
    expect(formatURLForDisplay("")).toBe("");
  });
});
