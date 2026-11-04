#!/usr/bin/env python3
"""Удаляет из таблиц стилей селекторы с указанными мёртвыми классами.

Зачем: при переделке каркаса админки классы старой оболочки перестают
использоваться, и проверка check-dead-css.mjs справедливо падает. Удалять
сотни правил руками долго и рискованно, поэтому скрипт убирает из списков
селекторов только те, где встречается мёртвый класс, а правило целиком —
когда в нём не осталось ни одного селектора. Пустые @media тоже удаляются.

Использование: prune-css.py файл.css [файл.css …] -- класс [класс …]
"""
import re
import sys


def split_top(text):
    """Делит текст на элементы верхнего уровня: (prelude, body) или комментарий."""
    items, i, n = [], 0, len(text)
    while i < n:
        if text.startswith("/*", i):
            j = text.index("*/", i) + 2
            items.append(("comment", text[i:j]))
            i = j
            continue
        if text[i].isspace():
            j = i
            while j < n and text[j].isspace():
                j += 1
            items.append(("space", text[i:j]))
            i = j
            continue
        j = text.find("{", i)
        semi = text.find(";", i)
        if semi != -1 and (j == -1 or semi < j):
            items.append(("stmt", text[i : semi + 1]))
            i = semi + 1
            continue
        depth, k = 0, j
        while k < n:
            if text.startswith("/*", k):
                k = text.index("*/", k) + 2
                continue
            if text[k] == "{":
                depth += 1
            elif text[k] == "}":
                depth -= 1
                if depth == 0:
                    break
            k += 1
        items.append(("rule", text[i:j], text[j + 1 : k]))
        i = k + 1
    return items


def split_selectors(prelude):
    parts, depth, cur = [], 0, ""
    for ch in prelude:
        if ch == "(":
            depth += 1
        elif ch == ")":
            depth -= 1
        if ch == "," and depth == 0:
            parts.append(cur)
            cur = ""
        else:
            cur += ch
    parts.append(cur)
    return parts


def prune(text, dead):
    pattern = re.compile(r"\.(" + "|".join(map(re.escape, dead)) + r")(?![\w-])")
    out = []
    for item in split_top(text):
        if item[0] != "rule":
            out.append(item[1])
            continue
        prelude, body = item[1], item[2]
        if prelude.lstrip().startswith("@"):
            if prelude.lstrip().startswith(("@media", "@supports", "@layer", "@container")):
                inner = prune(body, dead)
                if inner.strip():
                    out.append(prelude + "{" + inner + "}")
                else:
                    # убираем и отступ перед пустым блоком
                    while out and out[-1].isspace():
                        out.pop()
            else:
                out.append(prelude + "{" + body + "}")
            continue
        selectors = [s for s in split_selectors(prelude) if not pattern.search(s)]
        if not selectors:
            while out and out[-1].isspace() and "\n" in out[-1]:
                out.pop()
                out.append("\n")
                break
            continue
        kept = ",".join(selectors)
        if not kept.startswith(("\n", " ")) and out and not out[-1].endswith(("\n", " ")):
            kept = "\n" + kept
        out.append(kept.lstrip("\n") if out and out[-1].endswith("\n") else kept)
        out.append("{" + body + "}")
    result = "".join(out)
    return re.sub(r"\n{3,}", "\n\n", result)


if __name__ == "__main__":
    args = sys.argv[1:]
    split = args.index("--")
    files, dead = args[:split], args[split + 1 :]
    for path in files:
        source = open(path, encoding="utf-8").read()
        pruned = prune(source, dead)
        if pruned != source:
            open(path, "w", encoding="utf-8").write(pruned)
            print("обновлён", path)
