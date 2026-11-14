import assert from "node:assert/strict";
import test from "node:test";
import { variantGallery, variantMediaToPersist } from "../src/galleryOrder.ts";

test("shared cover appears in every gallery but is persisted once at product scope", () => {
  const cover = {
    id: "cover",
    serverId: 7,
    source: "https://images.example/cover.jpg",
    type: "image/jpeg",
    name: "Cover",
  };
  const first = {
    id: "first-photo",
    serverId: 8,
    source: "https://images.example/first.jpg",
    type: "image/jpeg",
    name: "First",
  };
  const second = {
    ...first,
    id: "second-photo",
    serverId: 9,
    source: "https://images.example/second.jpg",
  };
  const draft = {
    media: [cover],
    cover,
    variants: [
      { id: "first", media: [first], mediaCustomized: true },
      {
        id: "second",
        media: [{ ...cover, id: "old-cover-copy", serverId: 10 }, second],
        mediaCustomized: true,
      },
    ],
  };
  assert.deepEqual(variantGallery(draft, "first"), [cover, first]);
  assert.deepEqual(variantGallery(draft, "second"), [cover, second]);
  assert.deepEqual(variantMediaToPersist(draft, "first"), [first]);
  assert.deepEqual(variantMediaToPersist(draft, "second"), [second]);
  assert.deepEqual(
    variantMediaToPersist(
      { ...draft, variants: [{ id: "first", media: [], mediaCustomized: true }] },
      "first",
    ),
    [],
  );
});
