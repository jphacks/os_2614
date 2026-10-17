"use client";

import { Fragment, type ReactNode } from "react";

import {
  parseMarkdown,
  type Inline,
  type MarkdownBlock,
} from "@/data/markdown";
import styles from "./MarkdownBody.module.css";

export type MarkdownBodyProps = {
  source: string;
  className?: string;
};

const HEADING_TAGS = ["h1", "h2", "h3", "h4", "h5", "h6"] as const;

const SAFE_PROTOCOLS = new Set(["http:", "https:", "mailto:"]);

// javascript: などの危険なスキームはリンクにしない (plain text として描画する)。
function isSafeHref(href: string): boolean {
  try {
    return SAFE_PROTOCOLS.has(new URL(href).protocol.toLowerCase());
  } catch {
    return false;
  }
}

function assertNever(value: never): never {
  throw new Error(`unhandled markdown variant: ${String(value)}`);
}

function renderInlineList(nodes: Inline[]): ReactNode {
  return nodes.map((node, index) => (
    <Fragment key={index}>{renderInline(node)}</Fragment>
  ));
}

function renderInline(node: Inline): ReactNode {
  switch (node.type) {
    case "text":
      return node.value;
    case "strong":
      return <strong>{renderInlineList(node.children)}</strong>;
    case "em":
      // 日本語では斜体が読みにくいので、引用符ではなく控えめな色で示す。
      return <em className={styles.em}>{renderInlineList(node.children)}</em>;
    case "code":
      return <code className={styles.codeInline}>{node.value}</code>;
    case "strike":
      return <s>{renderInlineList(node.children)}</s>;
    case "link":
      if (!isSafeHref(node.href)) {
        return renderInlineList(node.children);
      }
      return (
        <a
          className={styles.link}
          href={node.href}
          target="_blank"
          rel="noreferrer noopener"
        >
          {renderInlineList(node.children)}
        </a>
      );
    case "math":
      return <code className={styles.math}>{node.tex}</code>;
    default:
      return assertNever(node);
  }
}

function renderBlock(block: MarkdownBlock, key: number): ReactNode {
  switch (block.type) {
    case "heading": {
      const HeadingTag = HEADING_TAGS[block.level - 1];
      return (
        <HeadingTag key={key} className={styles.heading}>
          {renderInlineList(block.children)}
        </HeadingTag>
      );
    }
    case "paragraph":
      return (
        <p key={key} className={styles.paragraph}>
          {renderInlineList(block.children)}
        </p>
      );
    case "code":
      return (
        <pre key={key} className={styles.codeBlock}>
          <code>{block.text}</code>
        </pre>
      );
    case "blockquote":
      return (
        <blockquote key={key} className={styles.blockquote}>
          {block.children.map((child, index) => renderBlock(child, index))}
        </blockquote>
      );
    case "list": {
      const items = block.items.map((item, index) => (
        <li key={index} className={styles.listItem}>
          {renderInlineList(item)}
        </li>
      ));
      return block.ordered ? (
        <ol key={key} className={styles.list}>
          {items}
        </ol>
      ) : (
        <ul key={key} className={styles.list}>
          {items}
        </ul>
      );
    }
    case "table":
      return (
        <div key={key} className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                {block.header.map((cell, index) => (
                  <th key={index} className={styles.tableHeaderCell}>
                    {renderInlineList(cell)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row, rowIndex) => (
                <tr key={rowIndex}>
                  {row.map((cell, cellIndex) => (
                    <td key={cellIndex} className={styles.tableCell}>
                      {renderInlineList(cell)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case "hr":
      return <hr key={key} className={styles.hr} />;
    case "mathBlock":
      return (
        <div key={key} className={styles.mathBlock}>
          <code>{block.tex}</code>
        </div>
      );
    default:
      return assertNever(block);
  }
}

export function MarkdownBody({ source, className }: MarkdownBodyProps) {
  const blocks = parseMarkdown(source);

  return (
    <div className={[styles.root, className].filter(Boolean).join(" ")}>
      {blocks.map((block, index) => renderBlock(block, index))}
    </div>
  );
}
