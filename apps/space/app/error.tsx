/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// pace imports
import { FORUM_URL, SUPPORT_EMAIL } from "@pace/constants";
// ui
import { Button } from "@pace/propel/button";

// Where to send crash details is per-installation configuration, and this fork ships with neither a support address nor a forum. The whole request for details goes when there is no channel to ask for them on -- asking someone to write to an address that does not exist wastes the one moment they were willing to help -- and the wording follows whichever channel is left so the sentence stays grammatical on its own.
function ReportDetailsSentence() {
  if (!SUPPORT_EMAIL && !FORUM_URL) return null;

  const supportLink = SUPPORT_EMAIL ? (
    <a href={`mailto:${SUPPORT_EMAIL}`} className="text-accent-primary">
      {SUPPORT_EMAIL}
    </a>
  ) : null;
  const forumLink = FORUM_URL ? (
    <a href={FORUM_URL} target="_blank" className="text-accent-primary" rel="noopener noreferrer">
      Forum
    </a>
  ) : null;

  return (
    <>
      {" "}
      If you have more details, please {supportLink ? <>write to {supportLink}</> : null}
      {supportLink && forumLink ? " or " : null}
      {forumLink ? <>post on our {forumLink}</> : null}.
    </>
  );
}

function ErrorPage() {
  const handleRetry = () => {
    window.location.reload();
  };

  return (
    <div className="grid h-screen place-items-center bg-surface-1 p-4">
      <div className="space-y-8 text-center">
        <div className="space-y-2">
          <h3 className="text-16 font-semibold">Yikes! That doesn{"'"}t look good.</h3>
          <p className="mx-auto text-13 text-secondary md:w-1/2">
            That crashed Plane, pun intended. No worries, though. Our engineers have been notified.
            <ReportDetailsSentence />
          </p>
        </div>
        <div className="flex items-center justify-center gap-2">
          <Button variant="primary" size="lg" onClick={handleRetry}>
            Refresh
          </Button>
          {/* <Button variant="secondary" size="lg" onClick={() => {}}>
            Sign out
          </Button> */}
        </div>
      </div>
    </div>
  );
}

export default ErrorPage;
