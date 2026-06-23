# Publishing Apps to the Pennsieve App Store from GitHub

This guide walks you through connecting your GitHub account to Pennsieve and publishing an application to the Pennsieve App Store every time you cut a new GitHub release.

It is split into four parts:

1. [Linking your GitHub account to Pennsieve](#1-linking-your-github-account-to-pennsieve)
2. [Choosing which repositories Pennsieve tracks](#2-choosing-which-repositories-pennsieve-tracks)
3. [Publishing a repository to the App Store on each GitHub release](#3-publishing-a-repository-to-the-app-store-on-each-github-release)
4. [How application permissions work (in plain English)](#4-how-application-permissions-work-in-plain-english)

---

## 1. Linking your GitHub account to Pennsieve

Before Pennsieve can read your repositories or react to your releases, you need to tell GitHub that Pennsieve is allowed to act on your behalf. We do this once, through the **Pennsieve GitHub Application**.

### Where to find it in the app

1. Sign in to Pennsieve.
2. From the left navigation, open **My Workspace → Settings**.
3. Click **Integrations**. You'll see a card for each available integration (ORCID, GitHub, API Keys).
4. On the **GitHub** card, click **Connect GitHub**.

   *(URL: `/my-workspace/settings/integrations/github`)*

### What happens when you click Connect

Pennsieve opens a popup window to GitHub. From there:

1. GitHub asks you to sign in (if you aren't already).
2. GitHub asks you to install and authorize the **Pennsieve GitHub Application**.
3. GitHub asks you which repositories Pennsieve should be allowed to see:
   - **All repositories** — Pennsieve can see every repo you own, now and in the future.
   - **Only select repositories** — pick the specific repos you want to share with Pennsieve. *This is the recommended option.*
4. Click **Install & Authorize**.
5. The popup closes itself, and the GitHub card on the Integrations page now shows your GitHub username and a **Connected** badge.

Behind the scenes Pennsieve has:

- Exchanged the short-lived code GitHub returned for a long-lived access token.
- Looked up your GitHub profile (login, URL, avatar).
- Stored the access token and the GitHub **installation ID** against your Pennsieve user.

### Updating which repos Pennsieve sees

After you've connected, the GitHub integration card shows two buttons:

- **Manage GitHub** — opens the GitHub integration detail page on Pennsieve.
- **Update Integration** — re-opens the GitHub App install screen so you can add or remove repositories that Pennsieve can access. Use this any time you create a new repo that you want Pennsieve to track.

### Disconnecting

To remove the connection, open the GitHub detail page (**Manage GitHub**) and click the **delete** (trash) icon next to your GitHub username. A confirmation dialog explains what disconnecting does:

- Pennsieve will no longer track GitHub repositories in your account.
- GitHub releases will no longer result in Pennsieve GitHub publications.
- You can no longer create Applications and Workflows on Pennsieve.

You can re-link any time by repeating the connect flow.

### Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| The repo you want to publish isn't listed under **My Code** | The Pennsieve GitHub Application isn't installed on the repo. | Click **Update Integration** on the GitHub card and add the repo, or do it on GitHub at **Settings → Applications → Pennsieve**. |
| You see someone else's GitHub profile after connecting | You were signed in to the wrong GitHub account in your browser. | Click **delete** on the GitHub detail page to disconnect, sign out of GitHub, then reconnect. |
| The popup never sends you back | A browser popup blocker stopped it. | Allow popups for the Pennsieve domain and click **Connect GitHub** again. |

---

## 2. Choosing which repositories Pennsieve tracks

Once your GitHub account is linked, Pennsieve shows the repos the Pennsieve GitHub Application has access to under **My Workspace → My Code** (URL: `/my-workspace/code`).

The **My Code** page lists each repository with:

- The repo name and a link out to GitHub.
- A **Private** badge if it's private on GitHub.
- The primary **language** of the repo (e.g. `Python`, `Go`).
- A **Publishing Settings** box showing whether each destination is **On** or **Off** for that repo:
  - **Discover** — generate a citable DOI and a public landing page for each release.
  - **App Store** — register each release as a new version of an application in the Pennsieve App Store.

Use the **refresh** icon at the top of the list to pull the latest set of repos from GitHub (e.g. after you've added a new repo to the GitHub App installation).

### Turning on publishing for a repository

1. On the repo's row, click the **settings** (gear) icon inside the **Publishing Settings** box.
2. The **Publishing Settings** dialog opens.
3. Tick the destinations you want:
   - **Pennsieve Discover** — publish each release as an archived, citable dataset with a DOI and landing page.
   - **App Store** — publish each release as a new version of an app in the Pennsieve App Store.
4. Click **Save Settings**.

> **Status note:** the **App Store** option is currently labeled **Coming Soon** in the publishing dialog and is disabled — once it ships, the rest of the flow described in the next section runs automatically.

From this point forward, every release you publish on GitHub for this repo will produce a Pennsieve publication in the destinations you ticked.

---

## 3. Publishing a repository to the App Store on each GitHub release

Once App Store is turned on for a repo, the developer workflow is just normal GitHub — there's nothing to push from inside Pennsieve.

### How it works at a glance

```
You publish a GitHub release  ─►  GitHub notifies Pennsieve  ─►  Pennsieve builds the
                                                                  release as a new App
                                                                  Store version  ─►
                                                                  shows up in the catalog
```

### Required files in your repository

For Pennsieve to display and run the app, your repository must contain these files at the root on the tag being released:

| File | Purpose |
|---|---|
| `app.yml` | App metadata — name, description, command, inputs, outputs, language/runtime, etc. |
| `README.md` | Long-form description shown on the App Store detail page. |

These are pulled automatically from the release tag.

The `app.yml` file is also where you declare the runtime / language the app expects (e.g. `python`, `r`, `julia`, container base image). This is the field the App Store and Workflow Manager use to choose how to execute your app.

### Cutting a new release

1. Tag your commit (e.g. `v1.0.7`).
2. On GitHub, click **Releases → Draft a new release**, select the tag, fill in release notes, and click **Publish release**.
3. Pennsieve picks up the release automatically:
   - A new **version** entry is created for your app in the App Store, starting in `registering` state.
   - A background job clones the release tag, builds it, and pushes it to Pennsieve's container registry.
   - When the build succeeds, the version status flips to `deployed` and is runnable from the App Store.
4. You can watch progress under **App Store → \[your app\] → Versions**. Statuses move through:
   - `registering` → `building` → `deployed` (success), or
   - `failed` (with a reason — usually a missing `app.yml` or a build error).

### Private repositories

Private repos work exactly the same way. Pennsieve uses the access token stored when you linked your GitHub account, so the linked account must have access to the repo. If you lose access on GitHub (e.g. you leave the org that owns the repo), Pennsieve will no longer be able to build new releases.

### Updating a release

Pennsieve treats each release tag as immutable. To ship a fix, **cut a new release with a new tag** (e.g. `v1.0.8`). Re-tagging the same name on GitHub is not a supported way to update an existing App Store version.

### Turning off App Store publishing

Open the repo's **Publishing Settings** dialog from **My Code**, untick **App Store**, and save. New releases will no longer be ingested. Existing versions remain in the App Store for users who already have them.

---

## 4. How application permissions work (in plain English)

Permissions on an App Store application answer two questions:

1. **Who can *see* this app in the App Store?**
2. **Who can *change* this app's settings, sharing, or remove it?**

Pennsieve's model is intentionally simple. Here are the concepts.

### Who's involved

- **Owner** — the Pennsieve user who originally published the app. There is always exactly one owner. Only the owner can change visibility or share the app with others.
- **Workspace** — a Pennsieve organization/team space. Each app belongs to the workspace it was published in.
- **Shared users / teams / workspaces** — people, teams, or whole workspaces the owner has explicitly given access to. They can *see and use* the app but not change its settings.

### Visibility: public vs private

Every app is either **public** or **private**.

| Visibility | Who can see it in the App Store catalog? |
|---|---|
| **Public** | Everyone with a Pennsieve account. Think of this as listing your app in an open marketplace. |
| **Private** | Only the owner, plus the specific users, teams, and workspaces the owner has shared it with. |

You set this once when you publish the app and can change it any time from the app's permissions page.

### A useful analogy

Think of a published app like a **document in a shared drive**:

- **Public** = "Anyone with the link can view" — anybody in Pennsieve can find it.
- **Private** = "Restricted" — only you, plus people you explicitly invite, can see it.
- **Owner** = the person who created the document. They're the only one who can change sharing settings.
- **Shared with you** = your name was added to the list. You can open and use the doc, but not re-share it or delete it.

### What can each role do?

| Action | Owner | Shared user / team / workspace member | Anyone else |
|---|---|---|---|
| See the app in the App Store catalog | ✓ | ✓ | ✓ public only |
| Run / use the app | ✓ | ✓ | ✓ public only |
| Change the visibility (public ↔ private) | ✓ | ✗ | ✗ |
| Share with more users / teams | ✓ | ✗ | ✗ |
| Remove the app from the App Store | ✓ | ✗ | ✗ |
| Publish new versions (via GitHub releases) | ✓ (whoever owns the linked GitHub account) | ✗ | ✗ |

### Sharing with users, teams, or whole workspaces

When an app is **private**, the owner can grant access at three levels:

- **A user** — one named Pennsieve user gets access.
- **A team** — every member of that team gets access. If the team grows, new members inherit it automatically.
- **A workspace** — *everybody* in that workspace gets access. Use this when you want all colleagues in a workspace to be able to run the app without naming each one.

A user is considered to have access if **any one** of these is true: they're the owner, they were shared individually, they belong to a team that was shared with, or they belong to a workspace that was shared with.

### Practical examples

- **"I want this app available to anyone on Pennsieve."**
  Set visibility to **Public**. No sharing list needed.

- **"I want only my lab to see this app."**
  Set visibility to **Private** and share it with your **workspace**.

- **"I'm collaborating with two colleagues across two different organizations."**
  Set visibility to **Private** and add each colleague as a shared **user**.

- **"This app should be available to the entire 'Imaging' team plus one external reviewer."**
  Set visibility to **Private**, share with the **Imaging** team, and add the reviewer as a shared **user**.

### Changing your mind later

All of the above is editable. Open the app in the App Store, go to **Permissions**, change visibility or edit the share list, and save.

---

## Appendix — Where things live

| Concept | In the web app | Behind the scenes |
|---|---|---|
| Connect / disconnect GitHub | **My Workspace → Settings → Integrations → GitHub** | `github-service` (`POST /accounts/github/register`, `DELETE /accounts/github/user`) |
| Add / remove repos the App can see | **Update Integration** button on the GitHub card → GitHub install screen | GitHub App installation; Pennsieve stores only the installation ID |
| See tracked repos & toggle publishing | **My Workspace → My Code** | `codeReposModule/fetchMyRepos` |
| App Store catalog, versions, permissions | **App Store** | `app-deploy-service` (`POST /store`, `GET /store/...`, `PUT /store/{id}/permissions`) |
| Authorization rule | — | `CanAccessApp` in `app-deploy-service`: public ⇒ open; otherwise owner, user share, team share, or workspace share |
