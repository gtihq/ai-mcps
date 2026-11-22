#!/usr/bin/env node
// Publishes the artifacts built by .forgejo/workflows/release.yml to a rolling
// GitHub release. Uses the REST API with a short-lived installation token, so
// it works on the Forgejo runner image (which has Node but not gh).

import { readFile, readdir } from "node:fs/promises";
import { join } from "node:path";

const token = process.env.GH_TOKEN;
const repo = process.env.REPO || "gtihq/ai-mcps";
const tag = process.env.RELEASE_TAG || "master";
const dir = process.env.RELEASE_DIR || "release";
const dryRun = process.env.DRY_RUN === "1";
const api = "https://api.github.com";

if (!token && !dryRun) {
    throw new Error("GH_TOKEN is not set");
}

const headers = {
    Authorization: `Bearer ${token}`,
    Accept: "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
    "User-Agent": "ai-mcps-ci",
};

/**
 * Sends a request to the GitHub API and returns status, ok and parsed body.
 */
async function request(path, options = {}) {
    const response = await fetch(`${api}${path}`, {
        ...options,
        headers: { ...headers, ...(options.headers || {}) },
    });
    const text = await response.text();
    let body = null;
    if (text) {
        try {
            body = JSON.parse(text);
        } catch {
            body = text;
        }
    }
    return { status: response.status, ok: response.ok, body };
}

const files = (await readdir(dir)).filter((file) => !file.endsWith(".tmp")).sort();
const notes = await readFile(join(dir, "build-info.txt"), "utf8");

if (dryRun) {
    console.log(`DRY RUN: would publish ${files.length} files to ${repo}@${tag}`);
    console.log(files.map((file) => `  ${file}`).join("\n"));
    process.exit(0);
}

let release = await request(`/repos/${repo}/releases/tags/${tag}`);
if (release.status === 404) {
    const created = await request(`/repos/${repo}/releases`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
            tag_name: tag,
            name: `GTI AI MCPs (${tag})`,
            body: notes,
            draft: false,
            prerelease: false,
        }),
    });
    if (!created.ok) {
        throw new Error(`create release failed: ${created.status} ${JSON.stringify(created.body)}`);
    }
    release = created;
} else if (!release.ok) {
    throw new Error(`get release failed: ${release.status} ${JSON.stringify(release.body)}`);
} else {
    const edited = await request(`/repos/${repo}/releases/${release.body.id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: `GTI AI MCPs (${tag})`, body: notes }),
    });
    if (!edited.ok) {
        throw new Error(`edit release failed: ${edited.status} ${JSON.stringify(edited.body)}`);
    }
    release = edited;
}

const existing = await request(`/repos/${repo}/releases/${release.body.id}/assets?per_page=100`);
if (!existing.ok) {
    throw new Error(`list assets failed: ${existing.status} ${JSON.stringify(existing.body)}`);
}
for (const asset of existing.body || []) {
    if (!files.includes(asset.name)) {
        continue;
    }
    const removed = await request(`/repos/${repo}/releases/assets/${asset.id}`, { method: "DELETE" });
    if (!removed.ok) {
        throw new Error(`delete asset ${asset.name} failed: ${removed.status} ${JSON.stringify(removed.body)}`);
    }
}

for (const file of files) {
    const data = await readFile(join(dir, file));
    const uploadUrl = `${String(release.body.upload_url).replace("{?name,label}", "")}?name=${encodeURIComponent(file)}`;
    const response = await fetch(uploadUrl, {
        method: "POST",
        headers: {
            ...headers,
            "Content-Type": "application/octet-stream",
            "Content-Length": String(data.length),
        },
        body: data,
    });
    if (!response.ok) {
        throw new Error(`upload ${file} failed: ${response.status} ${(await response.text()).slice(0, 300)}`);
    }
    console.log(`uploaded ${file} (${data.length} bytes)`);
}

console.log(`Release ${tag}: https://github.com/${repo}/releases/tag/${tag}`);
