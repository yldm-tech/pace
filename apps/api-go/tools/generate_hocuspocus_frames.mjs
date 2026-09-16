/**
 * Record the frames the real Hocuspocus server writes.
 *
 * The Go port has to put the same bytes on the wire, because the client reading them is the unmodified `@hocuspocus/provider`. So every frame the server can send is built here with the server's own encoder and written down as base64.
 *
 * Run from the repository root:
 *
 *     pnpm install --filter @plane/live...
 *     node apps/api-go/tools/generate_hocuspocus_frames.mjs > apps/api-go/internal/live/hocuspocus/testdata/frames.json
 */

import { createRequire } from "node:module";
import path from "node:path";

// Resolved against the live service, because that is the workspace package that depends on Hocuspocus.
const require = createRequire(path.join(process.cwd(), "apps/live/package.json"));
const { OutgoingMessage } = require("@hocuspocus/server");
const Y = require("yjs");

const NAME = "11111111-1111-4111-8111-111111111111";

// A document with something in it, so the state vector a first sync step carries is not the empty one.
//
// The client id is fixed rather than drawn at random, because it is encoded into both the state vector and the update and a fresh one on every run would make this file undiffable.
const document = new Y.Doc();
document.clientID = 4242;
document.getXmlFragment("default").insert(0, [new Y.XmlElement("paragraph")]);

const frames = [
  { name: "sync", bytes: new OutgoingMessage(NAME).createSyncMessage().toUint8Array() },
  { name: "sync reply", bytes: new OutgoingMessage(NAME).createSyncReplyMessage().toUint8Array() },
  { name: "query awareness", bytes: new OutgoingMessage(NAME).writeQueryAwareness().toUint8Array() },
  { name: "authenticated read write", bytes: new OutgoingMessage(NAME).writeAuthenticated(false).toUint8Array() },
  { name: "authenticated readonly", bytes: new OutgoingMessage(NAME).writeAuthenticated(true).toUint8Array() },
  { name: "permission denied", bytes: new OutgoingMessage(NAME).writePermissionDenied("Authentication unsuccessful").toUint8Array() },
  { name: "stateless", bytes: new OutgoingMessage(NAME).writeStateless('{"action":"error"}').toUint8Array() },
  { name: "stateless with unicode", bytes: new OutgoingMessage(NAME).writeStateless("héllo ☃ 🎉").toUint8Array() },
  { name: "broadcast stateless", bytes: new OutgoingMessage(NAME).writeBroadcastStateless("locked").toUint8Array() },
  { name: "sync status saved", bytes: new OutgoingMessage(NAME).writeSyncStatus(true).toUint8Array() },
  { name: "sync status unsaved", bytes: new OutgoingMessage(NAME).writeSyncStatus(false).toUint8Array() },
  { name: "first sync step", bytes: new OutgoingMessage(NAME).createSyncMessage().writeFirstSyncStepFor(document).toUint8Array() },
  { name: "update", bytes: new OutgoingMessage(NAME).createSyncMessage().writeUpdate(Y.encodeStateAsUpdate(document)).toUint8Array() },
  // A name long enough that its length needs two bytes, which is where a varint that only ever wrote one would be caught.
  { name: "long document name", bytes: new OutgoingMessage("d".repeat(300)).writeStateless("x").toUint8Array() },
  { name: "unicode document name", bytes: new OutgoingMessage("página ☃").writeStateless("x").toUint8Array() },
  { name: "empty stateless payload", bytes: new OutgoingMessage(NAME).writeStateless("").toUint8Array() },
];

const cases = frames.map((frame) => ({
  name: frame.name,
  frame: Buffer.from(frame.bytes).toString("base64"),
}));

process.stdout.write(`${JSON.stringify(cases, null, 2)}\n`);
