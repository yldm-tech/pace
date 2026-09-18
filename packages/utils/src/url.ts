/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

const LOCALHOST_ADDRESSES = new Set(["localhost", "127.0.0.1", "0.0.0.0"]);
// IPv4 regex - matches 0.0.0.0 to 255.255.255.255
const IPV4_REGEX = /^(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$/;
// IPv6 regex - comprehensive pattern for all IPv6 formats
const IPV6_REGEX =
  /^(?:(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}|(?:[0-9a-fA-F]{1,4}:){1,7}:|(?:[0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|(?:[0-9a-fA-F]{1,4}:){1,5}(?::[0-9a-fA-F]{1,4}){1,2}|(?:[0-9a-fA-F]{1,4}:){1,4}(?::[0-9a-fA-F]{1,4}){1,3}|(?:[0-9a-fA-F]{1,4}:){1,3}(?::[0-9a-fA-F]{1,4}){1,4}|(?:[0-9a-fA-F]{1,4}:){1,2}(?::[0-9a-fA-F]{1,4}){1,5}|[0-9a-fA-F]{1,4}:(?::[0-9a-fA-F]{1,4}){1,6}|:(?::[0-9a-fA-F]{1,4}){1,7}|::|fe80:(?::[0-9a-fA-F]{0,4}){0,4}%[0-9a-zA-Z]{1,}|::(?:ffff(?::0{1,4}){0,1}:){0,1}(?:(?:25[0-5]|(?:2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(?:25[0-5]|(?:2[0-4]|1{0,1}[0-9]){0,1}[0-9])|(?:[0-9a-fA-F]{1,4}:){1,4}:(?:(?:25[0-5]|(?:2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(?:25[0-5]|(?:2[0-4]|1{0,1}[0-9]){0,1}[0-9]))$/;

/**
 * Checks if a string is a valid IPv4 address
 * @param ip - String to validate as IPv4
 * @returns True if valid IPv4 address
 */
export function isValidIPv4(ip: string): boolean {
  if (!ip || typeof ip !== "string") return false;
  return IPV4_REGEX.test(ip);
}

/**
 * Checks if a string is a valid IPv6 address
 * @param ip - String to validate as IPv6
 * @returns True if valid IPv6 address
 */
export function isValidIPv6(ip: string): boolean {
  if (!ip || typeof ip !== "string") return false;

  // Remove brackets if present (for URL format like [::1])
  const cleanIP = ip.replace(/^\[|\]$/g, "");

  return IPV6_REGEX.test(cleanIP);
}

/**
 * Checks if a string is a valid IP address (IPv4 or IPv6)
 * @param ip - String to validate as IP address
 * @returns Object with validation results
 */
export function validateIPAddress(ip: string): {
  isValid: boolean;
  type: "ipv4" | "ipv6" | "invalid";
  formatted?: string;
} {
  if (!ip || typeof ip !== "string") {
    return { isValid: false, type: "invalid" };
  }

  if (isValidIPv4(ip)) {
    return { isValid: true, type: "ipv4", formatted: ip };
  }

  if (isValidIPv6(ip)) {
    const formatted = ip.replace(/^\[|\]$/g, ""); // Remove brackets
    return { isValid: true, type: "ipv6", formatted };
  }

  return { isValid: false, type: "invalid" };
}

/**
 * Checks if a URL string points to a localhost address.
 * @param url - The URL string to check
 * @returns True if the URL points to localhost, false otherwise
 */
export function isLocalhost(url: string): boolean {
  const hostname = extractHostname(url);
  return LOCALHOST_ADDRESSES.has(hostname);
}

/**
 * Extracts hostname from a URL string by removing protocol, path, query, hash, and port.
 * @param url - The URL string to extract hostname from
 * @returns The cleaned hostname
 */
export function extractHostname(url: string): string {
  let hostname = url;

  // Remove protocol if present
  if (hostname.includes("://")) {
    hostname = hostname.split("://")[1];
  }

  // Remove auth credentials if present
  const atIndex = hostname.indexOf("@");
  if (atIndex !== -1) {
    hostname = hostname.substring(atIndex + 1);
  }

  // Remove path, query, hash, and port in one pass
  hostname = hostname.split("/")[0].split("?")[0].split("#")[0].split(":")[0];

  return hostname;
}

/**
 * Returns a readable representation of a URL by stripping the protocol
 * and any trailing slash. For valid URLs, only the host is returned.
 * Invalid URLs are sanitized by removing the protocol and trailing slash.
 *
 * @param url - The URL string to format
 * @returns The formatted domain for display
 */
export function formatURLForDisplay(url: string): string {
  if (!url) return "";

  try {
    return new URL(url).host;
  } catch (_error) {
    return extractHostname(url);
  }
}

/**
 * Validates that a next_path parameter is safe for redirection.
 * Only allows relative paths starting with "/" to prevent open redirect vulnerabilities.
 *
 * @param url - The next_path URL to validate
 * @returns True if the URL is a safe relative path, false otherwise
 *
 * @example
 * isValidNextPath("/dashboard") // true
 * isValidNextPath("/workspace/123") // true
 * isValidNextPath("https://malicious.com") // false
 * isValidNextPath("//malicious.com") // false (protocol-relative)
 * isValidNextPath("javascript:alert(1)") // false
 * isValidNextPath("") // false
 * isValidNextPath("dashboard") // false (must start with /)
 * isValidNextPath("\\malicious") // false (backslash)
 * isValidNextPath("  /dashboard  ") // true (trimmed)
 */
export function isValidNextPath(url: string): boolean {
  if (!url || typeof url !== "string") return false;

  // Trim leading/trailing whitespace
  const trimmedUrl = url.trim();

  if (!trimmedUrl) return false;

  // Only allow relative paths starting with /
  if (!trimmedUrl.startsWith("/")) return false;

  // Block protocol-relative URLs (//example.com) - open redirect vulnerability
  if (trimmedUrl.startsWith("//")) return false;

  // Block backslashes which can be used for path traversal or Windows-style paths
  if (trimmedUrl.includes("\\")) return false;

  try {
    // Use URL constructor with a dummy base to normalize and validate the path
    const normalizedUrl = new URL(trimmedUrl, "http://localhost");

    // Ensure the path is still relative (no host change from our dummy base)
    if (normalizedUrl.hostname !== "localhost" || normalizedUrl.protocol !== "http:") {
      return false;
    }

    // Use the normalized pathname for additional security checks
    const pathname = normalizedUrl.pathname;

    // Additional security checks for malicious patterns in the normalized path
    const maliciousPatterns = [
      /javascript:/i,
      /data:/i,
      /vbscript:/i,
      /<script/i,
      /on\w+=/i, // Event handlers like onclick=, onload=
    ];

    return !maliciousPatterns.some((pattern) => pattern.test(pathname));
  } catch (_error) {
    // If URL constructor fails, it's an invalid path
    return false;
  }
}

/**
 * Ensures that a URL has a trailing slash while preserving query parameters and fragments
 * @param url - The URL to process
 * @returns The URL with a trailing slash added to the pathname (if not already present)
 */
export function ensureTrailingSlash(url: string): string {
  try {
    const fallbackBaseUrl =
      typeof window !== "undefined" && window.location.origin ? window.location.origin : "http://dummy.com";
    // Handle relative URLs by creating a URL object with a fallback base URL
    const urlObj = new URL(url, fallbackBaseUrl);

    // Don't modify root path
    if (urlObj.pathname === "/") {
      return url;
    }

    // Add trailing slash if it doesn't exist
    if (!urlObj.pathname.endsWith("/")) {
      urlObj.pathname += "/";
    }

    // For relative URLs, return just the path + search + hash
    if (url.startsWith("/")) {
      return urlObj.pathname + urlObj.search + urlObj.hash;
    }

    // For absolute URLs, return the full URL
    return urlObj.toString();
  } catch (error) {
    // If URL parsing fails, return the original URL
    console.warn("Failed to parse URL for trailing slash enforcement:", url, error);
    return url;
  }
}
