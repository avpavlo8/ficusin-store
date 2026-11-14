import assert from "node:assert/strict";
import test from "node:test";
import {
  appendGalleryMedia,
  replaceVariantGallery,
  variantGallery,
  commonCover,
} from "../src/galleryOrder.ts";

const photo = (id, source) => ({
  id,
  ...(source ? { source } : {}),
  name: id,
  type: "image/jpeg",
});
const shared = [photo("cover", "/cover.jpg"), photo("second", "/second.jpg")];
const added = photo("upload");
const initial = () => ({
  media: shared,
  variants: [
    { id: "one", media: [] },
    { id: "two", media: [] },
  ],
});

test("upload keeps inherited photos but only changes the selected variant", () => {
  const draft = initial();
  const next = { ...draft, ...appendGalleryMedia(draft, "one", added) };
  const last = { ...next, ...appendGalleryMedia(next, "one", photo("another")) };
  assert.deepEqual(
    variantGallery(last, "one").map((item) => item.id),
    ["cover", "second", "upload", "another"],
  );
  assert.deepEqual(variantGallery(last, "two"), shared);
  assert.deepEqual(last.media, shared);
  assert.deepEqual(draft.variants[0].media, []);
});

test("cover changes are shared while variant photos stay separate after reload", () => {
  const draft = initial();
  const uploaded = { ...draft, ...appendGalleryMedia(draft, "one", added) };
  const changed = { ...uploaded, ...replaceVariantGallery(uploaded, "one", [added, ...shared]) };
  assert.equal(commonCover(changed).id, "upload");
  assert.equal(variantGallery(changed, "one")[0].id, "upload");
  assert.equal(variantGallery(changed, "two")[0].id, "upload");
  const restored = JSON.parse(JSON.stringify(changed));
  assert.equal(commonCover(restored).id, "upload");
  assert.equal(variantGallery(restored, "two")[0].id, "upload");
});

test("deleting a variant photo leaves the common cover and other variants intact", () => {
  const draft = initial();
  const uploaded = { ...draft, ...appendGalleryMedia(draft, "one", added) };
  const removed = { ...uploaded, ...replaceVariantGallery(uploaded, "one", [shared[0]]) };
  assert.deepEqual(variantGallery(removed, "one"), [shared[0]]);
  assert.deepEqual(variantGallery(removed, "two"), shared);
});

test("an existing private gallery keeps its files and uploads separate", () => {
  const own = photo("own", "/own.jpg");
  const draft = {
    media: shared,
    variants: [
      { id: "one", media: [own] },
      { id: "two", media: [] },
    ],
  };
  const next = { ...draft, ...appendGalleryMedia(draft, "one", added) };
  assert.deepEqual(variantGallery(next, "one"), [shared[0], own, added]);
  assert.deepEqual(variantGallery(next, "two"), shared);
});
