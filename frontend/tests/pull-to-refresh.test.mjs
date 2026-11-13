import assert from "node:assert/strict";
import test from "node:test";
import { installPullToRefresh } from "../src/lib/pullToRefresh.ts";

function fixture({ enabled = true } = {}) {
  const attributes = new Map();
  const node = (parentElement = null, patch = {}) => ({
    nodeType: 1,
    isConnected: true,
    parentElement,
    scrollTop: 0,
    scrollHeight: 100,
    clientHeight: 100,
    scrollWidth: 100,
    clientWidth: 100,
    style: { overflowX: "visible", overflowY: "visible", overscrollBehaviorY: "auto" },
    closest(selector) {
      for (let current = this; current; current = current.parentElement) {
        if (current.control && selector.includes(current.control)) return current;
      }
      return null;
    },
    getAttribute: (key) => attributes.get(key) ?? null,
    setAttribute: (key, value) => attributes.set(key, value),
    removeAttribute: (key) => attributes.delete(key),
    ...patch,
  });
  const root = node();
  const body = node(root);
  const target = node(body);
  const listeners = new Map();
  const options = new Map();
  const doc = {
    nodeType: 9,
    documentElement: root,
    body,
    scrollingElement: root,
    visibilityState: "visible",
    blocker: null,
    querySelector() {
      return this.blocker;
    },
    addEventListener(name, fn, opts) {
      listeners.set(name, fn);
      options.set(name, opts);
    },
    removeEventListener(name, fn) {
      if (listeners.get(name) === fn) listeners.delete(name);
    },
  };
  const win = { scrollY: 0, getComputedStyle: (element) => element.style };
  const progress = [];
  let reloads = 0;
  let refresh = () => {
    reloads++;
  };
  const dispose = installPullToRefresh({
    window: win,
    document: doc,
    enabled,
    onProgress: (distance) => progress.push(distance),
    onRefresh: () => refresh(),
  });
  const touch = (x, y, identifier = 1) => ({ clientX: x, clientY: y, identifier });
  const emit = (name, points = [], at = target, extra = {}) => {
    const event = {
      touches: points,
      defaultPrevented: false,
      cancelable: true,
      composedPath: () => [at, body, root, doc],
      preventDefault() {
        this.defaultPrevented = true;
      },
      ...extra,
    };
    listeners.get(name)?.(event);
    return event;
  };
  const begin = (at = target) => emit("touchstart", [touch(20, 50)], at);
  const move = (distance, x = 20, extra = {}) =>
    emit("touchmove", [touch(x, 50 + distance)], target, extra);
  const end = () => emit("touchend");
  return {
    node,
    root,
    body,
    target,
    doc,
    win,
    options,
    listeners,
    attributes,
    progress,
    emit,
    touch,
    begin,
    move,
    end,
    dispose,
    get reloads() {
      return reloads;
    },
    setRefresh(fn) {
      refresh = fn;
    },
  };
}

test("installed handler cancels the first downward move and refreshes once on release", async () => {
  const f = fixture();
  assert.equal(f.options.get("touchstart").passive, true);
  assert.equal(f.options.get("touchmove").passive, false);
  assert.equal(f.attributes.get("data-pull-refresh"), "true");
  f.begin();
  assert.equal(f.move(2).defaultPrevented, true, "iOS must not take over the initial move");
  f.move(110);
  assert.equal(f.reloads, 0, "crossing the threshold alone must not reload");
  f.end();
  f.end();
  assert.equal(f.reloads, 1);
  await Promise.resolve();
  assert.equal(f.progress.at(-1), 0);
  f.dispose();
  assert.equal(f.listeners.size, 0);
  assert.equal(f.attributes.has("data-pull-refresh"), false);
});

test("ordinary browser mode installs nothing and does not change scroll behaviour", () => {
  const f = fixture({ enabled: false });
  f.begin();
  f.move(160);
  f.end();
  assert.equal(f.reloads, 0);
  assert.equal(f.listeners.size, 0);
  assert.equal(f.attributes.size, 0);
});

test("short pulls, reversed pulls, horizontal swipes and native-owned gestures never reload", () => {
  for (const gesture of [
    (f) => f.move(50),
    (f) => {
      f.move(110);
      f.move(30);
    },
    (f) => {
      f.move(-10);
      f.move(130);
    },
    (f) => f.move(110, 180),
    (f) => f.move(110, 20, { cancelable: false }),
    (f) => f.move(110, 20, { defaultPrevented: true }),
  ]) {
    const f = fixture();
    f.begin();
    gesture(f);
    f.end();
    assert.equal(f.reloads, 0);
    f.dispose();
  }
});

test("page and nested scrollers must begin at the top", () => {
  for (const configure of [
    (f) => {
      f.win.scrollY = 30;
    },
    (f) => {
      f.root.scrollTop = 30;
    },
    (f) =>
      Object.assign(f.target, { scrollTop: 20, scrollHeight: 300, style: { overflowY: "auto" } }),
    (f) =>
      Object.assign(f.target, {
        scrollHeight: 300,
        style: { overflowY: "auto", overscrollBehaviorY: "contain" },
      }),
    (f) => Object.assign(f.target, { scrollWidth: 300, style: { overflowX: "auto" } }),
  ]) {
    const f = fixture();
    configure(f);
    f.begin();
    assert.equal(f.move(130).defaultPrevented, false);
    f.end();
    assert.equal(f.reloads, 0);
    f.dispose();
  }
  const f = fixture();
  Object.assign(f.target, {
    scrollHeight: 300,
    style: { overflowY: "auto", overscrollBehaviorY: "auto" },
  });
  f.begin();
  f.move(110);
  f.end();
  assert.equal(f.reloads, 1);
  f.dispose();
});

test("inputs, content editing, photo dragging, modals and active saves own their gestures", () => {
  for (const control of [
    "input",
    "textarea",
    "select",
    "contenteditable",
    "textbox",
    "slider",
    "listbox",
    "draggable",
    "data-pull-refresh-ignore",
  ]) {
    const f = fixture();
    f.target.control = control;
    f.begin();
    f.move(130);
    f.end();
    assert.equal(f.reloads, 0, control);
    f.dispose();
  }
  const f = fixture();
  f.doc.blocker = {};
  f.begin();
  f.move(130);
  f.end();
  assert.equal(f.reloads, 0);
  f.doc.blocker = null;
  f.begin();
  f.move(130);
  f.doc.blocker = {};
  f.end();
  assert.equal(f.reloads, 0, "save began while pulling");
  f.dispose();
});

test("pinch, touch cancellation, removed targets and hiding the app cancel a pull", () => {
  for (const cancel of [
    (f) => f.emit("touchmove", [f.touch(20, 180), f.touch(80, 180, 2)]),
    (f) => f.emit("touchcancel"),
    (f) => {
      f.target.isConnected = false;
    },
    (f) => {
      f.doc.visibilityState = "hidden";
      f.emit("visibilitychange");
    },
  ]) {
    const f = fixture();
    f.begin();
    f.move(130);
    cancel(f);
    f.end();
    assert.equal(f.reloads, 0);
    f.dispose();
  }
});

test("a pending refresh ignores additional pulls, and cleanup is safe for StrictMode", async () => {
  const f = fixture();
  let release;
  let calls = 0;
  f.setRefresh(() => {
    calls++;
    return new Promise((resolve) => {
      release = resolve;
    });
  });
  f.begin();
  f.move(130);
  f.end();
  f.begin();
  f.move(140);
  f.end();
  assert.equal(calls, 1, "another pull started a duplicate refresh");
  f.dispose();
  const updates = f.progress.length;
  release();
  await Promise.resolve();
  assert.equal(f.listeners.size, 0);
  assert.equal(f.progress.length, updates, "disposed async work must not update the indicator");
  const next = fixture();
  next.begin();
  next.move(130);
  next.end();
  assert.equal(next.reloads, 1);
  next.dispose();
});
