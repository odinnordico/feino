// Register common highlight.js languages for syntax highlighting in code blocks.
// highlight.js/styles/github-dark.css is imported in main.tsx.

import hljs from "highlight.js/lib/core";

import bash       from "highlight.js/lib/languages/bash";
import go         from "highlight.js/lib/languages/go";
import javascript from "highlight.js/lib/languages/javascript";
import typescript from "highlight.js/lib/languages/typescript";
import python     from "highlight.js/lib/languages/python";
import rust       from "highlight.js/lib/languages/rust";
import json       from "highlight.js/lib/languages/json";
import yaml       from "highlight.js/lib/languages/yaml";
import sql        from "highlight.js/lib/languages/sql";
import xml        from "highlight.js/lib/languages/xml";
import css        from "highlight.js/lib/languages/css";
import markdown   from "highlight.js/lib/languages/markdown";

hljs.registerLanguage("bash",       bash);
hljs.registerLanguage("sh",         bash);
hljs.registerLanguage("go",         go);
hljs.registerLanguage("javascript", javascript);
hljs.registerLanguage("js",         javascript);
hljs.registerLanguage("typescript", typescript);
hljs.registerLanguage("ts",         typescript);
hljs.registerLanguage("python",     python);
hljs.registerLanguage("py",         python);
hljs.registerLanguage("rust",       rust);
hljs.registerLanguage("rs",         rust);
hljs.registerLanguage("json",       json);
hljs.registerLanguage("yaml",       yaml);
hljs.registerLanguage("yml",        yaml);
hljs.registerLanguage("sql",        sql);
hljs.registerLanguage("xml",        xml);
hljs.registerLanguage("html",       xml);
hljs.registerLanguage("css",        css);
hljs.registerLanguage("markdown",   markdown);
hljs.registerLanguage("md",         markdown);

export default hljs;
