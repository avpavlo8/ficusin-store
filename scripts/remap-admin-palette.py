#!/usr/bin/env python3
"""Переводит цвета стилей админки в нейтральную палитру «лёгкой» админки.

Зачем: за год в стилях админки накопилось больше тысячи цветов, прописанных
вручную, — бежевые подложки, оливковые зелёные, ржаво-красные пометки.
Владелец решил (4.11.2026): белый и холодные серые, один насыщенный зелёный
акцент, янтарный для «нужно сделать», без красного. Переписывать каждое
правило руками долго и неточно, поэтому скрипт пересчитывает каждый цвет,
сохраняя его светлоту (контраст текста и фона не меняется):

- почти серые и бежевые  → холодный нейтральный серый той же светлоты;
- зелёные и оливковые    → изумрудный оттенок той же светлоты;
- красные, ржавые, оранжевые и охристые → янтарный той же светлоты;
- синие и прочие          → без изменений.

Скрипт идемпотентен в пределах округления: повторный запуск почти ничего
не меняет. Использование: remap-admin-palette.py файл.css [файл.tsx …]
"""
import colorsys
import re
import sys

HEX = re.compile(r"#([0-9a-fA-F]{6}|[0-9a-fA-F]{3})\b")
RGBA = re.compile(r"rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*(,\s*[\d.]+\s*)?\)")


def remap(r, g, b):
    h, l, s = colorsys.rgb_to_hls(r / 255, g / 255, b / 255)
    deg = h * 360
    if s < 0.22 or (l > 0.9 and s < 0.6):
        # Нейтральный: холодный серый, очень слабая насыщенность.
        nh, ns = 220 / 360, min(0.09, 0.09 * (1 - abs(l - 0.5) * 1.2))
    elif 60 <= deg < 175:
        # Зелёный → изумрудный #11804f (hue ≈ 154).
        nh, ns = 154 / 360, max(0.55, min(s, 0.85))
        if l > 0.85:
            ns = 0.45
        elif 0.27 < l < 0.5:
            # Средние зелёные — это текст и кнопки: темнее, чтобы контраст
            # с белым и со светло-зелёной подложкой был не ниже 4.5:1.
            l = 0.27
    elif deg < 60 or deg >= 330:
        # Красный, ржавый, оранжевый, охра → янтарный.
        nh, ns = 34 / 360, max(0.75, min(s, 1.0))
        if l > 0.85:
            # Светлые терракотовые — это бывшие рамки и подложки; в новой
            # палитре они нейтральные, янтарь оставляем только пометкам.
            nh, ns = 220 / 360, 0.08
        elif 0.28 < l < 0.62:
            # Бывшие ржавые кнопки и пометки: тёмный янтарь читается и белым
            # текстом на нём, и им самим на светлом фоне.
            l = 0.28
    else:
        return None
    nr, ng, nb = colorsys.hls_to_rgb(nh, l, ns)
    return round(nr * 255), round(ng * 255), round(nb * 255)


def hex_sub(match):
    value = match.group(1)
    if len(value) == 3:
        value = "".join(ch * 2 for ch in value)
    r, g, b = (int(value[i : i + 2], 16) for i in (0, 2, 4))
    mapped = remap(r, g, b)
    if not mapped:
        return match.group(0)
    return "#%02x%02x%02x" % mapped


def rgba_sub(match):
    r, g, b = (int(match.group(i)) for i in (1, 2, 3))
    mapped = remap(r, g, b)
    if not mapped:
        return match.group(0)
    alpha = match.group(4) or ""
    name = "rgba" if alpha else "rgb"
    return f"{name}({mapped[0]}, {mapped[1]}, {mapped[2]}{alpha})"


for path in sys.argv[1:]:
    text = open(path, encoding="utf-8").read()
    new = RGBA.sub(rgba_sub, HEX.sub(hex_sub, text))
    if new != text:
        open(path, "w", encoding="utf-8").write(new)
        print("обновлён", path)
