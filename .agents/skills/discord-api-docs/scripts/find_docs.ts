function localPath(url: URL) {
  return decodeURIComponent(url.pathname).replace(/^\/([A-Za-z]:)/, "$1");
}

const refsDir = localPath(new URL("../references/", import.meta.url)).replace(
  /[\\/]$/,
  "",
);
const indexPath = `${refsDir}/doc-index.jsonl`;
const statePath = `${refsDir}/state.json`;

type Entry = {
  path: string;
  route: string;
  title: string;
  description: string;
  headings: string[];
  summary: string;
  searchText?: string;
};

function usage() {
  console.error(
    'Usage: deno run --allow-read scripts/find_docs.ts [--json] "query terms"',
  );
  Deno.exit(2);
}

const args = [...Deno.args];
const json = args.includes("--json");
const query = args.filter((arg) => arg !== "--json").join(" ").trim();
if (!query) usage();

function terms(input: string) {
  return input.toLowerCase().match(/[a-z0-9_./{}:-]+/g) ?? [];
}

function score(entry: Entry, queryTerms: string[]) {
  const title = entry.title.toLowerCase();
  const path = entry.path.toLowerCase();
  const route = entry.route.toLowerCase();
  const headings = entry.headings.join(" ").toLowerCase();
  const body = `${entry.description} ${entry.summary}`.toLowerCase();
  const searchText = (entry.searchText ?? "").toLowerCase();
  let total = 0;
  for (const term of queryTerms) {
    if (route.includes(term)) total += 8;
    if (path.includes(term)) total += 6;
    if (title.includes(term)) total += 5;
    if (headings.includes(term)) total += 3;
    if (searchText.includes(term)) {
      total += 2 + Math.min(occurrences(searchText, term), 5);
    }
    if (body.includes(term)) total += 1;
  }
  const phrase = queryTerms.join(" ");
  if (phrase && `${title} ${headings} ${body} ${searchText}`.includes(phrase)) {
    total += 10 + Math.min(occurrences(searchText, phrase), 5) * 2;
  }
  return total;
}

function occurrences(haystack: string, needle: string) {
  if (!needle) return 0;
  let count = 0;
  let index = haystack.indexOf(needle);
  while (index !== -1) {
    count++;
    index = haystack.indexOf(needle, index + needle.length);
  }
  return count;
}

function publicEntry(entry: Entry) {
  const { searchText: _searchText, ...rest } = entry;
  return rest;
}

async function readState() {
  try {
    return JSON.parse(await Deno.readTextFile(statePath));
  } catch {
    return null;
  }
}

try {
  const raw = await Deno.readTextFile(indexPath);
  const entries = raw.trim().split(/\r?\n/).filter(Boolean).map((line) =>
    JSON.parse(line) as Entry
  );
  const queryTerms = terms(query);
  const results = entries
    .map((entry) => ({ entry, score: score(entry, queryTerms) }))
    .filter((result) => result.score > 0)
    .sort((a, b) =>
      b.score - a.score || a.entry.path.localeCompare(b.entry.path)
    )
    .slice(0, 12);
  const state = await readState();

  if (json) {
    console.log(JSON.stringify(
      {
        query,
        state,
        results: results.map(({ entry, score }) => ({
          entry: publicEntry(entry),
          score,
        })),
      },
      null,
      2,
    ));
  } else {
    if (state) {
      console.log(`Docs commit: ${state.commit} synced ${state.syncedAt}`);
      console.log(`Repo: ${state.repoDir}`);
    }
    for (const { entry, score } of results) {
      console.log(`\n[${score}] ${entry.title}`);
      console.log(`  ${entry.path}`);
      console.log(`  ${entry.route}`);
      if (entry.headings.length) {
        console.log(`  headings: ${entry.headings.slice(0, 8).join(" | ")}`);
      }
      if (entry.summary) console.log(`  ${entry.summary.slice(0, 220)}`);
    }
    if (results.length === 0) {
      console.log(
        "No matches. Run sync_docs.ts first or try endpoint paths/object names literally.",
      );
    }
  }
} catch (error) {
  console.error(`Cannot read ${indexPath}. Run sync_docs.ts first.`);
  if (error instanceof Error) console.error(error.message);
  Deno.exit(1);
}
