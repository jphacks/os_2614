// 依存なしの Markdown パーサー。純関数で、React にも副作用にも依存しない。
// セキュリティ要件: 生 HTML は決して解釈しない (spec 10 §3:36)。
// `<script>`・`<img onerror=...>`・`<div>` などはすべて text Inline になる。
// HTML ブロック型は存在しない。

export type Inline =
  | { type: "text"; value: string }
  | { type: "strong"; children: Inline[] }
  | { type: "em"; children: Inline[] }
  | { type: "code"; value: string }
  | { type: "strike"; children: Inline[] }
  | { type: "link"; href: string; children: Inline[] }
  | { type: "math"; tex: string };

export type HeadingLevel = 1 | 2 | 3 | 4 | 5 | 6;

export type MarkdownBlock =
  | { type: "heading"; level: HeadingLevel; children: Inline[] }
  | { type: "paragraph"; children: Inline[] }
  | { type: "code"; lang: string; text: string }
  | { type: "blockquote"; children: MarkdownBlock[] }
  | { type: "list"; ordered: boolean; items: Inline[][] }
  | { type: "table"; header: Inline[][]; rows: Inline[][][] }
  | { type: "hr" }
  | { type: "mathBlock"; tex: string };

type InlineMatch = {
  node: Inline;
  length: number;
};

type ParsedBlock = {
  block: MarkdownBlock;
  next: number;
};

const HEADING_LEVELS: readonly HeadingLevel[] = [1, 2, 3, 4, 5, 6];

const HEADING_RE = /^(#{1,6})(?:[ \t]+(.*))?$/;
const FENCE_RE = /^ {0,3}(`{3,}|~{3,})(.*)$/;
const BLOCKQUOTE_RE = /^ {0,3}>[ \t]?(.*)$/;
const UNORDERED_RE = /^ {0,3}[-*+][ \t]+(.*)$/;
const ORDERED_RE = /^ {0,3}\d{1,9}[.)][ \t]+(.*)$/;
const HR_RE = /^ {0,3}([-*_])(?:[ \t]*\1){2,}[ \t]*$/;
const TABLE_SEPARATOR_CELL_RE = /^:?-{1,}:?$/;
const INDENTED_RE = /^[ \t]+\S/;
const ESCAPABLE = new Set("\\`*_{}[]()#+-.!|~$>".split(""));

// 極端に入れ子になった入力でもスタックを溢れさせないための上限。
const MAX_NESTING = 24;

function isBlank(line: string): boolean {
  return line.trim() === "";
}

function headingLevel(hash: string): HeadingLevel {
  return HEADING_LEVELS[hash.length - 1] ?? 1;
}

function isWordChar(char: string | undefined): boolean {
  return char !== undefined && /[0-9A-Za-z]/.test(char);
}

function pushText(nodes: Inline[], value: string): void {
  if (value === "") {
    return;
  }
  const last = nodes.at(-1);
  if (last?.type === "text") {
    last.value += value;
    return;
  }
  nodes.push({ type: "text", value });
}

function findDelimiter(text: string, from: number, delimiter: string): number {
  const width = delimiter.length;
  let index = from;

  while (index < text.length) {
    if (text[index] === "\\") {
      index += 2;
      continue;
    }
    if (text.startsWith(delimiter, index)) {
      // `***` のような長い連続は、delimiter と同じ長さのときだけ閉じる。
      let run = width;
      while (text[index + run] === delimiter[width - 1]) {
        run += 1;
      }
      if (run === width) {
        return index;
      }
      index += run;
      continue;
    }
    index += 1;
  }

  return -1;
}

function matchCode(text: string, index: number): InlineMatch {
  let run = 1;
  while (text[index + run] === "`") {
    run += 1;
  }
  const marker = "`".repeat(run);
  const close = text.indexOf(marker, index + run);

  if (close === -1) {
    // 閉じないバッククォートは、そのまま普通の文字として扱う。
    return { node: { type: "text", value: marker }, length: run };
  }

  let value = text.slice(index + run, close);
  if (
    value.length >= 2 &&
    value.startsWith(" ") &&
    value.endsWith(" ") &&
    value.trim() !== ""
  ) {
    value = value.slice(1, -1);
  }

  return { node: { type: "code", value }, length: close + run - index };
}

function matchMath(text: string, index: number): InlineMatch | null {
  const delimiter = text.startsWith("$$", index) ? "$$" : "$";
  const close = findDelimiter(text, index + delimiter.length, delimiter);
  if (close === -1) {
    return null;
  }

  const content = text.slice(index + delimiter.length, close);
  if (content === "" || content.includes("\n")) {
    return null;
  }
  // `$5 と $6` のような通貨表記を数式にしない。
  if (/^\s/.test(content) || /\s$/.test(content)) {
    return null;
  }

  return {
    node: { type: "math", tex: content },
    length: close + delimiter.length - index,
  };
}

function matchEmphasis(
  text: string,
  index: number,
  depth: number,
): InlineMatch | null {
  const char = text[index];
  if (char !== "*" && char !== "_" && char !== "~") {
    return null;
  }
  if (char === "~" && !text.startsWith("~~", index)) {
    return null;
  }

  const width =
    char === "~" ? 2 : text.startsWith(char + char, index) ? 2 : 1;
  const delimiter = char.repeat(width);

  // `snake_case_name` の途中を強調にしない (GFM の intraword underscore 規則)。
  if (char === "_" && isWordChar(text[index - 1])) {
    return null;
  }

  const close = findDelimiter(text, index + width, delimiter);
  if (close === -1) {
    return null;
  }
  if (char === "_" && isWordChar(text[close + width])) {
    return null;
  }

  const inner = text.slice(index + width, close);
  if (inner === "") {
    return null;
  }

  const fallback: Inline = { type: "text", value: inner };
  const children: Inline[] =
    depth < MAX_NESTING ? parseInline(inner, depth + 1) : [fallback];
  const length = close + width - index;

  if (char === "~") {
    return { node: { type: "strike", children }, length };
  }
  if (width === 2) {
    return { node: { type: "strong", children }, length };
  }
  return { node: { type: "em", children }, length };
}

function findClosingBracket(text: string, openIndex: number): number {
  let depth = 0;
  let index = openIndex;

  while (index < text.length) {
    const char = text[index];
    if (char === "\\") {
      index += 2;
      continue;
    }
    if (char === "[") {
      depth += 1;
    }
    if (char === "]") {
      depth -= 1;
      if (depth === 0) {
        return index;
      }
    }
    index += 1;
  }

  return -1;
}

function matchLink(
  text: string,
  index: number,
  depth: number,
): InlineMatch | null {
  const closeBracket = findClosingBracket(text, index);
  if (closeBracket === -1 || text[closeBracket + 1] !== "(") {
    return null;
  }

  const closeParen = text.indexOf(")", closeBracket + 2);
  if (closeParen === -1) {
    return null;
  }

  const label = text.slice(index + 1, closeBracket);
  if (label === "") {
    return null;
  }

  const href = text.slice(closeBracket + 2, closeParen).trim();
  const fallback: Inline = { type: "text", value: label };
  const children: Inline[] =
    depth < MAX_NESTING ? parseInline(label, depth + 1) : [fallback];

  return {
    node: { type: "link", href, children },
    length: closeParen - index + 1,
  };
}

function parseInline(text: string, depth = 0): Inline[] {
  const nodes: Inline[] = [];
  let plain = "";
  let index = 0;

  while (index < text.length) {
    const char = text[index];
    if (char === undefined) {
      break;
    }

    if (char === "\\" && index + 1 < text.length) {
      const next = text[index + 1];
      if (next !== undefined && ESCAPABLE.has(next)) {
        plain += next;
        index += 2;
        continue;
      }
    }

    let match: InlineMatch | null = null;
    if (char === "`") {
      match = matchCode(text, index);
    } else if (char === "$") {
      match = matchMath(text, index);
    } else if (char === "[") {
      match = matchLink(text, index, depth);
    } else if (char === "*" || char === "_" || char === "~") {
      match = matchEmphasis(text, index, depth);
    }

    if (match !== null) {
      if (plain !== "") {
        pushText(nodes, plain);
        plain = "";
      }
      nodes.push(match.node);
      index += match.length;
      continue;
    }

    plain += char;
    index += 1;
  }

  pushText(nodes, plain);
  return nodes;
}

function splitTableRow(line: string): string[] {
  let row = line.trim();
  if (row.startsWith("|")) {
    row = row.slice(1);
  }
  if (row.endsWith("|")) {
    row = row.slice(0, -1);
  }
  return row.split("|").map((cell) => cell.trim());
}

function isTableSeparator(line: string): boolean {
  if (!line.includes("|")) {
    return false;
  }
  const cells = splitTableRow(line);
  return (
    cells.length > 0 &&
    cells.every((cell) => TABLE_SEPARATOR_CELL_RE.test(cell))
  );
}

function isTableStart(lines: string[], index: number): boolean {
  const header = lines.at(index);
  const separator = lines.at(index + 1);
  if (header === undefined || separator === undefined) {
    return false;
  }
  if (!header.includes("|") || !isTableSeparator(separator)) {
    return false;
  }
  return splitTableRow(header).length === splitTableRow(separator).length;
}

function startsBlock(line: string): boolean {
  if (FENCE_RE.test(line)) {
    return true;
  }
  if (HEADING_RE.test(line)) {
    return true;
  }
  if (HR_RE.test(line)) {
    return true;
  }
  if (BLOCKQUOTE_RE.test(line)) {
    return true;
  }
  if (UNORDERED_RE.test(line) || ORDERED_RE.test(line)) {
    return true;
  }
  const trimmed = line.trim();
  return (
    trimmed === "$$" ||
    (trimmed.length > 4 && trimmed.startsWith("$$") && trimmed.endsWith("$$"))
  );
}

function isClosingFence(
  line: string,
  marker: string,
  minLength: number,
): boolean {
  const trimmed = line.trim();
  if (trimmed.length < minLength) {
    return false;
  }
  for (const char of trimmed) {
    if (char !== marker) {
      return false;
    }
  }
  return true;
}

function parseFence(lines: string[], start: number): ParsedBlock {
  const line = lines.at(start) ?? "";
  const match = FENCE_RE.exec(line);
  const fence = match?.[1] ?? "```";
  const marker = fence.charAt(0) === "~" ? "~" : "`";
  const info = (match?.[2] ?? "").trim();
  const lang = info.split(/[ \t]+/)[0] ?? "";
  const code: string[] = [];

  let index = start + 1;
  while (index < lines.length) {
    const current = lines.at(index);
    if (current === undefined) {
      break;
    }
    if (isClosingFence(current, marker, fence.length)) {
      index += 1;
      break;
    }
    code.push(current);
    index += 1;
  }

  return {
    block: { type: "code", lang, text: code.join("\n") },
    next: index,
  };
}

function parseTable(lines: string[], start: number): ParsedBlock | null {
  if (!isTableStart(lines, start)) {
    return null;
  }

  const headerLine = lines.at(start) ?? "";
  const headerCells = splitTableRow(headerLine);
  const rows: Inline[][][] = [];

  let index = start + 2;
  while (index < lines.length) {
    const rowLine = lines.at(index);
    if (rowLine === undefined) {
      break;
    }
    if (isBlank(rowLine) || !rowLine.includes("|") || startsBlock(rowLine)) {
      break;
    }
    const cells = splitTableRow(rowLine);
    rows.push(
      headerCells.map((_, cellIndex) =>
        parseInline(cells.at(cellIndex) ?? ""),
      ),
    );
    index += 1;
  }

  return {
    block: {
      type: "table",
      header: headerCells.map((cell) => parseInline(cell)),
      rows,
    },
    next: index,
  };
}

function parseBlockquote(
  lines: string[],
  start: number,
  depth: number,
): ParsedBlock {
  const inner: string[] = [];
  let index = start;

  while (index < lines.length) {
    const current = lines.at(index);
    if (current === undefined) {
      break;
    }
    const match = BLOCKQUOTE_RE.exec(current);
    if (match) {
      inner.push(match[1] ?? "");
      index += 1;
      continue;
    }
    if (isBlank(current)) {
      const next = lines.at(index + 1);
      if (next !== undefined && BLOCKQUOTE_RE.test(next)) {
        inner.push("");
        index += 1;
        continue;
      }
    }
    break;
  }

  const block: MarkdownBlock =
    depth < MAX_NESTING
      ? { type: "blockquote", children: parseBlocks(inner, depth + 1) }
      : { type: "paragraph", children: parseInline(inner.join(" ")) };

  return { block, next: index };
}

function parseList(lines: string[], start: number): ParsedBlock {
  const first = lines.at(start) ?? "";
  const ordered = ORDERED_RE.test(first);
  const pattern = ordered ? ORDERED_RE : UNORDERED_RE;
  const items: Inline[][] = [];
  let index = start;

  while (index < lines.length) {
    const current = lines.at(index);
    if (current === undefined) {
      break;
    }
    const match = pattern.exec(current);
    if (!match) {
      break;
    }

    const parts: string[] = [match[1] ?? ""];
    index += 1;

    // 継続行 (インデントされた行) は同じ項目のテキストとして連結する。
    while (index < lines.length) {
      const continuation = lines.at(index);
      if (continuation === undefined) {
        break;
      }
      if (isBlank(continuation)) {
        const after = lines.at(index + 1);
        if (
          after !== undefined &&
          INDENTED_RE.test(after) &&
          !pattern.test(after)
        ) {
          parts.push("");
          index += 1;
          continue;
        }
        break;
      }
      if (pattern.test(continuation) || !INDENTED_RE.test(continuation)) {
        break;
      }
      parts.push(continuation.trim());
      index += 1;
    }

    items.push(parseInline(parts.join(" ").trim()));
  }

  return { block: { type: "list", ordered, items }, next: index };
}

function parseParagraph(lines: string[], start: number): ParsedBlock {
  const parts: string[] = [];
  let index = start;

  while (index < lines.length) {
    const current = lines.at(index);
    if (current === undefined || isBlank(current)) {
      break;
    }
    if (index > start && (startsBlock(current) || isTableStart(lines, index))) {
      break;
    }
    parts.push(current.trim());
    index += 1;
  }

  return {
    block: { type: "paragraph", children: parseInline(parts.join(" ")) },
    next: index,
  };
}

function parseMathBlock(lines: string[], start: number): ParsedBlock | null {
  const line = lines.at(start) ?? "";
  const trimmed = line.trim();

  if (
    trimmed.length > 4 &&
    trimmed.startsWith("$$") &&
    trimmed.endsWith("$$")
  ) {
    return {
      block: { type: "mathBlock", tex: trimmed.slice(2, -2).trim() },
      next: start + 1,
    };
  }

  if (trimmed !== "$$") {
    return null;
  }

  let close = -1;
  for (let index = start + 1; index < lines.length; index += 1) {
    if ((lines.at(index) ?? "").trim() === "$$") {
      close = index;
      break;
    }
  }
  if (close === -1) {
    return null;
  }

  return {
    block: {
      type: "mathBlock",
      tex: lines
        .slice(start + 1, close)
        .join("\n")
        .trim(),
    },
    next: close + 1,
  };
}

function parseBlocks(lines: string[], depth: number): MarkdownBlock[] {
  const blocks: MarkdownBlock[] = [];
  let index = 0;

  while (index < lines.length) {
    const line = lines.at(index);
    if (line === undefined) {
      break;
    }
    if (isBlank(line)) {
      index += 1;
      continue;
    }

    if (FENCE_RE.test(line)) {
      const parsed = parseFence(lines, index);
      blocks.push(parsed.block);
      index = parsed.next;
      continue;
    }

    const heading = HEADING_RE.exec(line);
    if (heading) {
      const hash = heading[1] ?? "#";
      const content = (heading[2] ?? "")
        .replace(/[ \t]+#+[ \t]*$/, "")
        .trim();
      blocks.push({
        type: "heading",
        level: headingLevel(hash),
        children: parseInline(content),
      });
      index += 1;
      continue;
    }

    if (HR_RE.test(line)) {
      blocks.push({ type: "hr" });
      index += 1;
      continue;
    }

    if (BLOCKQUOTE_RE.test(line)) {
      const parsed = parseBlockquote(lines, index, depth);
      blocks.push(parsed.block);
      index = parsed.next;
      continue;
    }

    if (UNORDERED_RE.test(line) || ORDERED_RE.test(line)) {
      const parsed = parseList(lines, index);
      blocks.push(parsed.block);
      index = parsed.next;
      continue;
    }

    const math = parseMathBlock(lines, index);
    if (math !== null) {
      blocks.push(math.block);
      index = math.next;
      continue;
    }

    const table = parseTable(lines, index);
    if (table !== null) {
      blocks.push(table.block);
      index = table.next;
      continue;
    }

    const paragraph = parseParagraph(lines, index);
    blocks.push(paragraph.block);
    index = paragraph.next;
  }

  return blocks;
}

export function parseMarkdown(source: string): MarkdownBlock[] {
  try {
    const normalized = source.replace(/\r\n?/g, "\n");
    if (normalized.trim() === "") {
      return [];
    }
    return parseBlocks(normalized.split("\n"), 0);
  } catch {
    // 契約: どんな入力でも例外を投げない。
    return [];
  }
}
