package projects

// Template is the example projects file written by `lucid projects init`. It
// is kept identical to projects.example.yaml in the repository root (a test
// enforces this).
const Template = `# Lucidbench projects.
#
# This file is yours: it lives in your Lucidbench data directory (or wherever
# LUCID_PROJECTS points) and is never part of the repository. Lucidbench reads
# it to show what you build, what kind of thing each project is, and what
# needs what.
#
# category:   product | portfolio | tool | experiment | coursework
#             (a tool is a private project that builds other projects)
# type:       game | web-app | desktop-app | cli | library | service |
#             ml-research | site | video-system
# status:     idea | active | paused | frozen | shipped | archived
# visibility: public | private | confidential
#             (confidential projects are never sent to AI providers)
# needs:      status is todo | doing | done | blocked; from names the
#             project that supplies it, if any.
# local_path: optional absolute path of the project's checkout on this
#             machine. Work sessions need it; it is checked when used.
# deploy:     optional list of where it runs, so its card can show the live
#             deploy status from the Cloud extension: provider (gcloud |
#             wrangler | vercel), service (the Cloud Run service, Worker or
#             Pages project, or Vercel project name) and, for gcloud only,
#             region and project.

version: 1
projects:
  - id: my-app
    name: My App
    category: product
    type: web-app
    status: active
    visibility: public
    repo: you/my-app
    # local_path: <absolute path to your checkout>
    linear: APP-1
    # deploy:
    #   - { provider: vercel, service: my-app }
    summary: The thing people actually use.
    needs:
      - { what: "Release pipeline", from: build-tools, status: doing }
      - { what: "Pricing page", status: todo }

  - id: my-portfolio-site
    name: Portfolio site
    category: portfolio
    type: site
    status: shipped
    visibility: public
    repo: you/you.github.io
    summary: Shows off finished work.
    needs:
      - { what: "Screenshots of My App", from: my-app, status: blocked }

  - id: build-tools
    name: Build tools
    category: tool
    type: cli
    status: active
    visibility: private
    summary: Private scripts that build and release the other projects.
    builds_into: [my-app, my-portfolio-site]
`
