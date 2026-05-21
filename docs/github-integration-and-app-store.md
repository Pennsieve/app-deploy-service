# Publishing Apps to the Pennsieve App Store from GitHub

This guide walks you through connecting your GitHub account to Pennsieve and publishing an application to the Pennsieve App Store every time you cut a new GitHub release.

It is split into three parts:

1. [Linking your GitHub account to Pennsieve](#1-linking-your-github-account-to-pennsieve)
2. [Publishing a repository to the App Store on each GitHub release](#2-publishing-a-repository-to-the-app-store-on-each-github-release)
3. [How application permissions work (in plain English)](#3-how-application-permissions-work-in-plain-english)

---

## 1. Linking your GitHub account to Pennsieve

Before Pennsieve can read your repositories or react to your releases, you need to tell GitHub that Pennsieve is allowed to act on your behalf. We do this once, through the **Pennsieve GitHub App**.

### What you'll end up with

- Your GitHub username, avatar, and a secure access token stored against your Pennsieve user profile.
- The **Pennsieve GitHub App** installed on the repositories you want to share with Pennsieve.

### Step-by-step

1. **Sign in to Pennsieve** at the usual web app.
2. Open **Settings → Integrations → GitHub** (or whichever screen your workspace uses to manage integrations).
3. Click **Connect GitHub Account**.
   - You'll be redirected to GitHub.
   - GitHub will ask you to sign in (if you aren't already) and to approve the Pennsieve GitHub App.
4. **Choose which repositories Pennsieve can see.** GitHub will give you two options:
   - **All repositories** — Pennsieve can see every repo you own, now and in the future.
   - **Only select repositories** — pick the specific repos you want to publish to Pennsieve. *This is the recommended option.*
5. Click **Install & Authorize**.
6. GitHub sends you back to Pennsieve. Behind the scenes, Pennsieve:
   - Exchanges the short-lived code GitHub returned for a long-lived access token.
   - Looks up your GitHub profile (login, URL, avatar).
   - Stores the access token and the GitHub **installation ID** against your Pennsieve user.
7. You should now see your GitHub avatar and username on the integrations screen, with a **Connected** badge.

### Disconnecting

To remove the connection, click **Disconnect GitHub** on the same integrations screen. This:

- Removes the Pennsieve GitHub App from your selected repositories on the GitHub side.
- Deletes your stored GitHub profile and access token on the Pennsieve side.

You can re-link at any time by repeating the steps above.

### Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| "Repository not found" when publishing | The Pennsieve GitHub App is not installed on the repo. | Go to **GitHub → Settings → Applications → Pennsieve** and add the missing repo. |
| "Connect" button keeps re-prompting | A previous installation was deleted on GitHub but not on Pennsieve. | Click **Disconnect GitHub** and connect again. |
| You see someone else's GitHub profile | You signed in to a different GitHub account in your browser. | Sign out of GitHub, then click **Connect GitHub Account** again. |

---

## 2. Publishing a repository to the App Store on each GitHub release

Once your account is linked, you can opt **any repository** the Pennsieve GitHub App can see into the App Store. From then on, **every GitHub release** you publish on that repository becomes a new version of your app in the Pennsieve App Store.

### How it works at a glance

```
You publish a GitHub release  ─►  GitHub notifies Pennsieve  ─►  Pennsieve builds the
                                                                  release as a new App
                                                                  Store version  ─►
                                                                  shows up in the catalog
```

### One-time setup for a repository

1. In Pennsieve, go to **App Store → Add Application** (or **Publish Repository**).
2. Select the repository from the list of repos the Pennsieve GitHub App has access to.
   - If the repo you want isn't there, jump to **GitHub → Settings → Applications → Pennsieve** and add it to the installation, then refresh.
3. Choose whether the application should be **public** or **private** in the App Store.
   - This is a default for new versions. You can change it later — see the [permissions section](#3-how-application-permissions-work-in-plain-english).
4. Click **Enable**.

That's it. Your repository is now wired up.

### Required files in your repository

So Pennsieve has something to show in the App Store, your repository needs to contain these files at the root (on the tag you're releasing):

| File | Purpose |
|---|---|
| `application.json` | App metadata — name, description, command, inputs, outputs, etc. |
| `README.md` | Long-form description rendered on the App Store detail page. |

These are pulled automatically from the tag whenever a release is published.

### Cutting a new release

Once setup is done, the developer workflow is just normal GitHub:

1. Tag your commit (e.g. `v1.0.7`).
2. On GitHub, click **Releases → Draft a new release**, select the tag, fill in release notes, and click **Publish release**.
3. Pennsieve picks up the release automatically:
   - A new **version** entry is created for your app in the App Store, starting in `registering` state.
   - A background job clones the release tag, builds it, and pushes it to Pennsieve's container registry.
   - When the build succeeds, the version status flips to `deployed` and becomes runnable from the App Store.
4. You can watch progress in **App Store → \[your app\] → Versions**. The status will move through:
   - `registering` → `building` → `deployed` (success), or
   - `failed` (with a reason — usually a missing `application.json` or a build error).

### Private repositories

Private repos work exactly the same way. When the App Store needs to clone or read a private repo, it uses the access token Pennsieve stored when you linked your GitHub account, so make sure that account is the one that has access to the repo. If you lose access on GitHub (e.g. you leave the org that owns the repo), Pennsieve will no longer be able to build new releases.

### Updating a release

Pennsieve treats each release tag as immutable. If you need to ship a fix, **cut a new release with a new tag** (e.g. `v1.0.8`) rather than re-using `v1.0.7`. Re-tagging the same name in GitHub is not a supported way to update an existing App Store version.

### Removing a repository from the App Store

Open the application in the App Store and click **Remove from App Store**. Existing versions are kept for users who already have them, but no new releases will be ingested. To stop Pennsieve from seeing the repo entirely, remove it from the Pennsieve GitHub App installation on the GitHub side.

---

## 3. How application permissions work (in plain English)

Permissions on an App Store application answer two questions:

1. **Who can *see* this app in the App Store?**
2. **Who can *change* this app's settings, sharing, or remove it?**

Pennsieve's model is intentionally simple. Here are the concepts:

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

You set this once when you publish the app and can change it any time from the app's settings page.

### A useful analogy

Think of a published app like a **document in a shared drive**:

- **Public** = "Anyone with the link can view" — anybody in Pennsieve can find it.
- **Private** = "Restricted" — only you, plus people you explicitly invite, can see it.
- **Owner** = the person who created the document. They're the only one who can change sharing settings.
- **Shared with you** = your name was added to the list. You can open and use the doc, but not re-share it or delete it.

### What can each role do?

| Action | Owner | Shared user/team/workspace member | Anyone else |
|---|---|---|---|
| See the app in the App Store catalog | ✓ | ✓ (private apps) / ✓ (public apps) | ✓ public only |
| Run/use the app | ✓ | ✓ | ✓ public only |
| Change the visibility (public ↔ private) | ✓ | ✗ | ✗ |
| Share with more users/teams | ✓ | ✗ | ✗ |
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

All of the above is editable. Open the app in the App Store, go to **Permissions**, change visibility or edit the share list, and save. Existing users keep working until the next time their permissions are checked.

---

## Appendix — How this is wired up under the hood (for the curious)

- **GitHub account linking** is handled by `github-service`. The browser-side OAuth flow returns a short-lived `code` and `installation_id`. Pennsieve exchanges them for an access token and stores them on the user's profile.
- **App Store publishing** is handled by `app-deploy-service`. When a release event arrives, `POST /store` records the new application (if first time) and a new version row, then kicks off a Fargate task that clones the tag, builds the container, and pushes it to the Pennsieve registry.
- **Authorization** for everything App Store-related (catalog, registry, permissions) goes through the `CanAccessApp` check: public apps are open, otherwise the user must be the owner, or be listed individually, via a team, or via their workspace.
