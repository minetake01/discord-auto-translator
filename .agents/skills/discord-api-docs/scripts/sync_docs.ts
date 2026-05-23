function localPath(url: URL) {
  return decodeURIComponent(url.pathname).replace(/^\/([A-Za-z]:)/, "$1");
}

const defaultRepo = localPath(
  new URL("../references/discord-api-docs-repo", import.meta.url),
);
const repoDir = Deno.env.get("DISCORD_API_DOCS_REPO") ?? defaultRepo;
const refsDir = localPath(new URL("../references/", import.meta.url)).replace(
  /[\\/]$/,
  "",
);
const indexPath = `${refsDir}/doc-index.jsonl`;
const statePath = `${refsDir}/state.json`;
const upstream = "https://github.com/discord/discord-api-docs.git";
const branch = "main";

async function run(cmd: string[], cwd?: string) {
  const child = new Deno.Command(cmd[0], {
    args: cmd.slice(1),
    cwd,
    stdout: "piped",
    stderr: "piped",
  });
  const output = await child.output();
  const stdout = new TextDecoder().decode(output.stdout).trim();
  const stderr = new TextDecoder().decode(output.stderr).trim();
  if (!output.success) {
    throw new Error(`${cmd.join(" ")} failed\n${stderr || stdout}`);
  }
  return stdout;
}

async function exists(path: string) {
  try {
    await Deno.stat(path);
    return true;
  } catch (error) {
    if (error instanceof Deno.errors.NotFound) return false;
    throw error;
  }
}

function normalizePath(path: string) {
  return path.replaceAll("\\", "/");
}

async function walkDocs(root: string) {
  const files: string[] = [];
  const skip = new Set([".git", "node_modules", ".next", "dist", "build"]);

  async function visit(dir: string) {
    for await (const entry of Deno.readDir(dir)) {
      if (skip.has(entry.name)) continue;
      const full = `${dir}/${entry.name}`;
      if (entry.isDirectory) {
        await visit(full);
      } else if (entry.isFile && /\.(md|mdx)$/i.test(entry.name)) {
        files.push(full);
      }
    }
  }

  await visit(root);
  files.sort();
  return files;
}

function extractFrontmatter(text: string) {
  if (!text.startsWith("---")) return {};
  const end = text.indexOf("\n---", 3);
  if (end === -1) return {};
  const yaml = text.slice(3, end).trim();
  const data: Record<string, string> = {};
  for (const line of yaml.split(/\r?\n/)) {
    const match = line.match(/^([A-Za-z0-9_-]+):\s*["']?(.+?)["']?\s*$/);
    if (match) data[match[1]] = match[2];
  }
  return data;
}

function summarize(text: string) {
  return cleanSearchText(text)
    .trim()
    .slice(0, 500);
}

function cleanSearchText(text: string) {
  return text
    .replace(/^---[\s\S]*?\n---\s*/m, "")
    .replace(/\[\\?(\{[^}\]]+\})\\?\]\([^)]+\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/\\([{}[\]])/g, "$1")
    .replace(/<[^>\n]+>/g, " ")
    .replace(/[[\]`*_#>|]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function headings(text: string) {
  return [...text.matchAll(/^(#{1,4})\s+(.+)$/gm)]
    .slice(0, 40)
    .map((match) => match[2].replace(/\s*\{#.+?\}\s*$/, "").trim());
}

function routeFor(relativePath: string) {
  return "/" +
    relativePath.replace(/\.(md|mdx)$/i, "").replace(/\/index$/i, "");
}

async function syncRepo() {
  await Deno.mkdir(refsDir, { recursive: true });
  if (!(await exists(`${repoDir}/.git`))) {
    const parent = repoDir.replace(/[\\/][^\\/]+$/, "");
    await Deno.mkdir(parent, { recursive: true });
    await run([
      "git",
      "clone",
      "--depth",
      "1",
      "--branch",
      branch,
      upstream,
      repoDir,
    ]);
  } else {
    await run(["git", "fetch", "--depth", "1", "origin", branch], repoDir);
    await run(["git", "reset", "--hard", `origin/${branch}`], repoDir);
  }
}

async function buildIndex() {
  const commit = await run(["git", "rev-parse", "HEAD"], repoDir);
  const files = await walkDocs(repoDir);
  const lines: string[] = [];
  for (const file of files) {
    const text = await Deno.readTextFile(file);
    const rel = normalizePath(file.slice(repoDir.length + 1));
    const meta = extractFrontmatter(text);
    lines.push(JSON.stringify({
      path: rel,
      route: routeFor(rel),
      title: meta.title ?? headings(text)[0] ?? rel.split("/").at(-1),
      description: meta.description ?? "",
      headings: headings(text),
      summary: summarize(text),
      searchText: cleanSearchText(text),
    }));
  }
  await Deno.writeTextFile(indexPath, `${lines.join("\n")}\n`);
  await Deno.writeTextFile(
    statePath,
    JSON.stringify(
      {
        upstream,
        branch,
        commit,
        syncedAt: new Date().toISOString(),
        repoDir: normalizePath(repoDir),
        indexPath: normalizePath(indexPath),
        files: files.length,
      },
      null,
      2,
    ),
  );
  return { commit, files: files.length };
}

try {
  await syncRepo();
  const result = await buildIndex();
  console.log(
    `Synced ${result.files} docs files from ${upstream}#${branch} at ${result.commit}`,
  );
  console.log(`Repo: ${normalizePath(repoDir)}`);
  console.log(`Index: ${normalizePath(indexPath)}`);
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  Deno.exit(1);
}
