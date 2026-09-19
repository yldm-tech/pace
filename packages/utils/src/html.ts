/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// Entry point for `@pace/utils/html`, kept out of the package barrel so that sanitize-html is only downloaded by code that strips tags out of HTML. The barrel builds to a single bundled module, so a top-level import here would be a top-level import for every consumer of every utility in this package -- `cn` included, which every route uses. See ./markdown.ts for the same reasoning applied to the HTML-to-Markdown pipeline.

import sanitizeHtml from "sanitize-html";
import type { Content } from "@pace/types";
import { isJSONContentEmpty, truncateText } from "./string";

/**
 * @description : This function will remove all the HTML tags from the string
 * @param {string} htmlString
 * @return {string}
 * @example :
 * const html = "<p>Some text</p>";
const text = stripHTML(html);
console.log(text); // Some text
 */
export const sanitizeHTML = (htmlString: string) => {
  const sanitizedText = sanitizeHtml(htmlString, { allowedTags: [] }); // sanitize the string to remove all HTML tags
  return sanitizedText.trim(); // trim the string to remove leading and trailing whitespaces
};

/**
 * @description: This function will remove all the HTML tags from the string and truncate the string to the specified length
 * @param {string} html
 * @param {number} length
 * @return {string}
 * @example:
 * const html = "<p>Some text</p>";
 * const text = stripAndTruncateHTML(html);
 * console.log(text); // Some text
 */
export const stripAndTruncateHTML = (html: string, length: number = 55) => truncateText(sanitizeHTML(html), length);

export const isEmptyHtmlString = (htmlString: string, allowedHTMLTags: string[] = []) => {
  // Remove HTML tags using sanitize-html
  const cleanText = sanitizeHtml(htmlString, { allowedTags: allowedHTMLTags });
  // Trim the string and check if it's empty
  return cleanText.trim() === "";
};

/**
 * @description
 * This function will check if the comment is empty or not.
 * It returns true if comment is empty.
 * Now supports TipTap Content types (HTMLContent, JSONContent, JSONContent[], null)
 *
 * For HTML content:
 * 1. If comment is undefined/null
 * 2. If comment is an empty string
 * 3. If comment is "<p></p>"
 * 4. If comment contains only empty HTML tags
 *
 * For JSON content:
 * 1. If content is null/undefined
 * 2. If content has no meaningful text or nested content
 * 3. If all nested content is empty
 *
 * @param {Content} comment - TipTap Content type
 * @returns {boolean}
 */
export const isCommentEmpty = (comment: Content | undefined): boolean => {
  // Handle null/undefined
  if (!comment) return true;

  // Handle HTMLContent (string)
  if (typeof comment === "string") {
    return (
      comment.trim() === "" ||
      comment === "<p></p>" ||
      isEmptyHtmlString(comment, ["img", "mention-component", "image-component"])
    );
  }

  // Handle JSONContent[] (array)
  if (Array.isArray(comment)) {
    return comment.length === 0 || comment.every(isJSONContentEmpty);
  }

  // Handle JSONContent (object)
  return isJSONContentEmpty(comment);
};

/**
 * @description
 * Legacy function for backward compatibility with string comments
 * @param {string | undefined} comment
 * @returns {boolean}
 * @deprecated Use isCommentEmpty with Content type instead
 */
export const isStringCommentEmpty = (comment: string | undefined): boolean => {
  // return true if comment is undefined
  if (!comment) return true;
  return (
    comment?.trim() === "" ||
    comment === "<p></p>" ||
    isEmptyHtmlString(comment ?? "", ["img", "mention-component", "image-component", "embed-component"])
  );
};

export const sanitizeCommentForNotification = (mentionContent: string | undefined) =>
  mentionContent
    ? stripAndTruncateHTML(
        mentionContent.replace(/<mention-component\b[^>]*\blabel="([^"]*)"[^>]*><\/mention-component>/g, "$1")
      )
    : mentionContent;
