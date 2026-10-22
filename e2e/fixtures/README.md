`chat-video.webm` is a synthetic three-second clip used only by chat acceptance tests.
It shows a pale-green rectangle moving across a blue canvas. It contains no audio or customer content.
Created locally with Chromium canvas/MediaRecorder, then encoded as opaque VP8 with Playwright's bundled FFmpeg.

`chat-video.mp4` shows the same synthetic animation, recorded with WebKit's native MP4 MediaRecorder and normalized with macOS avconvert for Safari codec coverage.
