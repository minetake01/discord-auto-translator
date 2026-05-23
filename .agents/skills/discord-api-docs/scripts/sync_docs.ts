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

async function executableExists(path: string) {
  try {
    const stat = await Deno.stat(path);
    return stat.isFile;
  } catch (error) {
    if (error instanceof Deno.errors.NotFound) return false;
    throw error;
  }
}

async function resolveGit() {
  const configured = Deno.env.get("GIT");
  if (configured) return configured;

  if (Deno.build.os === "windows") {
    const candidates = [
      "C:\\Program Files\\Git\\bin\\git.exe",
      "C:\\Program Files (x86)\\Git\\bin\\git.exe",
    ];
    for (const candidate of candidates) {
      if (await executableExists(candidate)) return candidate;
    }
  }

  return "git";
}

async function run(cmd: string[], cwd?: string) {
  const child = new Deno.Command(cmd[0], {
    args: cmd.slice(1),
    cwd,
    stdout: "inherit",
    stderr: "inherit",
  });
  const output = await child.output();
  if (!output.success) {
    throw new Error(`${cmd.join(" ")} failed`);
  }
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
  const git = await resolveGit();
  const gitWithConfig = [git, "-c", "core.longpaths=true"];
  await Deno.mkdir(refsDir, { recursive: true });
  if (!(await exists(`${repoDir}/.git`))) {
    const parent = repoDir.replace(/[\\/][^\\/]+$/, "");
    await Deno.mkdir(parent, { recursive: true });
    await run([
      ...gitWithConfig,
      "clone",
      "--depth",
      "1",
      "--branch",
      branch,
      upstream,
      repoDir,
    ]);
  } else {
    await run(
      [...gitWithConfig, "fetch", "--depth", "1", "origin", branch],
      repoDir,
    );
    await run(
      [...gitWithConfig, "reset", "--hard", `origin/${branch}`],
      repoDir,
    );
  }
}

async function readHeadCommit() {
  const head = (await Deno.readTextFile(`${repoDir}/.git/HEAD`)).trim();
  if (/^[0-9a-f]{40}$/i.test(head)) return head;

  const refMatch = head.match(/^ref:\s+(.+)$/);
  if (!refMatch) throw new Error(`Cannot parse Git HEAD: ${head}`);

  const ref = refMatch[1].replaceAll("\\", "/");
  const refPath = `${repoDir}/.git/${ref}`;
  if (await exists(refPath)) {
    return (await Deno.readTextFile(refPath)).trim();
  }

  const packedRefsPath = `${repoDir}/.git/packed-refs`;
  if (await exists(packedRefsPath)) {
    const packedRefs = await Deno.readTextFile(packedRefsPath);
    for (const line of packedRefs.split(/\r?\n/)) {
      const [sha, packedRef] = line.trim().split(/\s+/);
      if (packedRef === ref && /^[0-9a-f]{40}$/i.test(sha)) return sha;
    }
  }

  throw new Error(`Cannot resolve Git ref: ${ref}`);
}

async function buildIndex() {
  const commit = await readHeadCommit();
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
