// The CLA check (workflow cla): every author of a pull request's commits has signed CLA.md,
// by posting PHRASE as a comment on a pull request. The signatures are kept in FILE on the
// branch BRANCH, which holds nothing else. The result is the commit status CONTEXT on the pull
// request's head, which main's ruleset requires, and one comment that says who is left.
//
// The workflow runs this file as the default branch has it, never as a pull request changes
// it, and reads nothing else of the pull request than its commits through the API.

const PHRASE = 'I have read the CLA Document and I hereby sign the CLA'
const RECHECK = 'recheck'
const BRANCH = 'cla-signatures'
const FILE = 'signatures.json'
const CONTEXT = 'CLA'
const MARKER = '<!-- cla-check -->'
// The CLA's beneficiary signs nothing to himself.
const EXEMPT = new Set(['vcanuel'])
const ATTEMPTS = 3

module.exports = async ({ github, context, core }) => {
  const { owner, repo } = context.repo
  const comment = context.eventName === 'issue_comment' ? context.payload.comment : null
  const said = comment ? comment.body.trim() : ''
  if (comment && (!context.payload.issue.pull_request || (said !== PHRASE && said !== RECHECK))) {
    return
  }
  const number = comment ? context.payload.issue.number : context.payload.pull_request.number
  const { data: pr } = await github.rest.pulls.get({ owner, repo, pull_number: number })
  if (pr.state !== 'open') return

  const authors = await commitAuthors(github, owner, repo, number)
  let signatures = await readSignatures(github, owner, repo)

  if (said === PHRASE) {
    const user = comment.user
    const isAuthor = authors.some((a) => a.id === user.id)
    if (isAuthor && !signatures.list.some((s) => s.id === user.id)) {
      signatures = await sign(github, owner, repo, {
        login: user.login,
        id: user.id,
        comment_id: comment.id,
        comment_url: comment.html_url,
        pull_request: number,
        created_at: comment.created_at,
      })
      core.info(`${user.login} signed the CLA`)
    }
  }

  const signed = new Set(signatures.list.map((s) => s.id))
  const missing = authors.filter((a) => a.id === null || (!EXEMPT.has(a.login) && !signed.has(a.id)))
  const cla = `${context.serverUrl}/${owner}/${repo}/blob/${pr.base.repo.default_branch}/CLA.md`

  await github.rest.repos.createCommitStatus({
    owner,
    repo,
    sha: pr.head.sha,
    state: missing.length ? 'failure' : 'success',
    context: CONTEXT,
    description: missing.length
      ? `${missing.length} author(s) of the commits must sign the CLA`
      : 'Every author of the commits has signed the CLA',
    target_url: cla,
  })
  await report(github, owner, repo, number, missing, cla)
}

// The authors of the pull request's commits: a GitHub account each, or, for a commit whose
// email no account claims, its name with a null id. Bots sign nothing.
async function commitAuthors(github, owner, repo, number) {
  const commits = await github.paginate(github.rest.pulls.listCommits, {
    owner,
    repo,
    pull_number: number,
    per_page: 100,
  })
  const authors = new Map()
  for (const c of commits) {
    if (c.author?.type === 'Bot') continue
    if (c.author) {
      authors.set(c.author.id, { login: c.author.login, id: c.author.id })
    } else {
      const name = c.commit.author?.name ?? 'unknown'
      authors.set(`unlinked:${name}`, { login: name, id: null, sha: c.sha })
    }
  }
  return [...authors.values()]
}

// The signatures and the blob they were read from; none while the branch does not exist.
async function readSignatures(github, owner, repo) {
  try {
    const { data } = await github.rest.repos.getContent({ owner, repo, path: FILE, ref: BRANCH })
    const parsed = JSON.parse(Buffer.from(data.content, 'base64').toString('utf8'))
    return { list: parsed.signatures ?? [], sha: data.sha }
  } catch (err) {
    if (err.status === 404) return { list: [], sha: null }
    throw err
  }
}

// Adds a signature, reading the file again when another run wrote it meanwhile.
async function sign(github, owner, repo, signature) {
  for (let attempt = 1; ; attempt++) {
    const current = await readSignatures(github, owner, repo)
    if (current.list.some((s) => s.id === signature.id)) return current
    const list = [...current.list, signature]
    const content = JSON.stringify({ signatures: list }, null, 2) + '\n'
    const message = `Record the CLA signature of @${signature.login}`
    try {
      if (current.sha) {
        await github.rest.repos.createOrUpdateFileContents({
          owner,
          repo,
          branch: BRANCH,
          path: FILE,
          message,
          content: Buffer.from(content).toString('base64'),
          sha: current.sha,
        })
      } else {
        await createBranch(github, owner, repo, content, message)
      }
      return { list, sha: null }
    } catch (err) {
      // 409: the file changed since it was read; 422: the branch was created meanwhile.
      if (attempt >= ATTEMPTS || (err.status !== 409 && err.status !== 422)) throw err
    }
  }
}

// The branch starts as a commit of its own, with no parent: it shares nothing with main.
async function createBranch(github, owner, repo, content, message) {
  const { data: tree } = await github.rest.git.createTree({
    owner,
    repo,
    tree: [{ path: FILE, mode: '100644', type: 'blob', content }],
  })
  const { data: commit } = await github.rest.git.createCommit({
    owner,
    repo,
    message,
    tree: tree.sha,
    parents: [],
  })
  await github.rest.git.createRef({ owner, repo, ref: `refs/heads/${BRANCH}`, sha: commit.sha })
}

// One comment per pull request, written while someone has to sign and updated once all have.
async function report(github, owner, repo, number, missing, cla) {
  const comments = await github.paginate(github.rest.issues.listComments, {
    owner,
    repo,
    issue_number: number,
    per_page: 100,
  })
  const previous = comments.find((c) => c.user?.type === 'Bot' && c.body?.includes(MARKER))
  if (!missing.length && !previous) return

  let body
  if (missing.length) {
    const toSign = missing.filter((a) => a.id !== null).map((a) => `@${a.login}`)
    const unlinked = missing.filter((a) => a.id === null)
    body = [
      MARKER,
      'Thank you for your pull request. Before it can be merged, every author of its commits signs',
      `Runsten's [Contributor License Agreement](${cla}), once for all their contributions. To sign,`,
      'read it, then post this comment, exactly:',
      '',
      '```',
      PHRASE,
      '```',
      '',
    ]
    if (toSign.length) body.push(`Still to sign: ${toSign.join(', ')}.`, '')
    for (const a of unlinked) {
      body.push(
        `Commit ${a.sha.slice(0, 7)} by ${a.login} has an email no GitHub account claims: add the`,
        'email to your account (Settings, Emails), or write the commit again with an email of it.',
        '',
      )
    }
    body.push(`Post \`${RECHECK}\` to check again.`)
  } else {
    body = [MARKER, 'Every author of the commits has signed the CLA. Thank you.']
  }
  body = body.join('\n')
  if (previous) {
    if (previous.body !== body) {
      await github.rest.issues.updateComment({ owner, repo, comment_id: previous.id, body })
    }
  } else {
    await github.rest.issues.createComment({ owner, repo, issue_number: number, body })
  }
}
