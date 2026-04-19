"use strict";

let ws = null;
let reconnectDelay = 1000;
let activeTab = "timeline";
let timeline = [];
let dateIndex = {};
let allDates = [];
let calendarMonth = null;
let findingsLibrary = [];
let findingsLoaded = false;
let searchTimeout = null;

const statusEl = document.getElementById("connection-status");
const searchShellEl = document.getElementById("search-shell");
const searchBoxEl = document.getElementById("search-box");
const pageSummaryEl = document.getElementById("page-summary");

const heroDaysEl = document.getElementById("hero-days");
const heroDaysNoteEl = document.getElementById("hero-days-note");
const heroFindingsEl = document.getElementById("hero-findings");
const heroFindingsNoteEl = document.getElementById("hero-findings-note");
const heroPRsEl = document.getElementById("hero-prs");
const heroPRsNoteEl = document.getElementById("hero-prs-note");
const heroLatestEl = document.getElementById("hero-latest");
const heroLatestNoteEl = document.getElementById("hero-latest-note");

const sliderEl = document.getElementById("timeline-slider");
const dateMinEl = document.getElementById("date-min");
const dateMaxEl = document.getElementById("date-max");
const dateCurrentEl = document.getElementById("date-current");
const prevDayBtn = document.getElementById("prev-day");
const nextDayBtn = document.getElementById("next-day");
const calGridEl = document.getElementById("calendar-grid");
const calLabelEl = document.getElementById("cal-month-label");
const calPrevBtn = document.getElementById("cal-prev");
const calNextBtn = document.getElementById("cal-next");
const chartBarEl = document.getElementById("chart-bar");
const chartDonutEl = document.getElementById("chart-doughnut");

const findingTotalEl = document.getElementById("finding-total");
const findingHighEl = document.getElementById("finding-high");
const findingProposedEl = document.getElementById("finding-proposed");
const findingLatestEl = document.getElementById("finding-latest");
const findingSearchEl = document.getElementById("finding-search");
const findingDispositionEl = document.getElementById("finding-disposition");
const findingCategoryEl = document.getElementById("finding-category");
const findingSortEl = document.getElementById("finding-sort");
const findingsSummaryEl = document.getElementById("findings-summary");
const findingsLibraryEl = document.getElementById("findings-library");

function clearChildren(el) {
  while (el.firstChild) {
    el.removeChild(el.firstChild);
  }
}

function connect() {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  ws = new WebSocket(proto + "//" + location.host + "/ws");

  ws.onopen = function () {
    reconnectDelay = 1000;
    statusEl.textContent = "Connected";
    statusEl.className = "connected";
    if (activeTab === "findings" && !findingsLoaded) {
      ensureFindingsLoaded();
    }
  };

  ws.onclose = function () {
    statusEl.textContent = "Disconnected";
    statusEl.className = "disconnected";
    setTimeout(connect, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, 30000);
  };

  ws.onerror = function () {
    ws.close();
  };

  ws.onmessage = function (event) {
    let msg;
    try {
      msg = JSON.parse(event.data);
    } catch (_) {
      return;
    }
    handleMessage(msg);
  };
}

function send(message) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(message));
  }
}

function handleMessage(msg) {
  switch (msg.type) {
    case "init":
      handleInit(msg);
      break;
    case "day_detail":
      handleDayDetail(msg);
      break;
    case "findings_library":
      handleFindingsLibrary(msg);
      break;
    case "search_results":
      handleSearchResults(msg);
      break;
    case "opportunities":
      handleOpportunities(msg);
      break;
    case "revenue_update":
      handleRevenueUpdate(msg);
      break;
    case "proposal_ready":
      handleProposalReady();
      break;
    default:
      break;
  }
}

function activateTab(tab) {
  activeTab = tab;

  document.querySelectorAll(".tab-btn").forEach(function (btn) {
    btn.classList.toggle("active", btn.getAttribute("data-tab") === tab);
  });

  document.querySelectorAll(".tab-content").forEach(function (panel) {
    panel.classList.toggle("hidden", panel.id !== "tab-" + tab);
  });

  searchShellEl.classList.toggle("hidden", tab !== "timeline");
  updatePageSummary(tab);

  if (tab === "findings") {
    ensureFindingsLoaded();
  } else if (tab === "opportunities") {
    send({ type: "list_opportunities", filter: "all", sort: "rank" });
  }
}

function updatePageSummary(tab) {
  const summaries = {
    timeline:
      "Trace day-by-day activity across findings, linked PRs, commits, and institutional memory search.",
    findings:
      "Filter the full research log by category, disposition, risk, or score and jump straight to source material.",
    opportunities:
      "Review the revenue pipeline, open listings, proposal drafts, and source suggestions from one place.",
  };
  pageSummaryEl.textContent = summaries[tab] || "";
}

document.querySelectorAll(".tab-btn").forEach(function (btn) {
  btn.addEventListener("click", function () {
    activateTab(this.getAttribute("data-tab"));
  });
});

function handleInit(msg) {
  timeline = msg.timeline || [];
  allDates = timeline.map(function (entry) {
    return entry.date;
  });
  dateIndex = {};
  allDates.forEach(function (date, idx) {
    dateIndex[date] = idx;
  });

  const range = msg.range || {};
  dateMinEl.textContent = range.min || "";
  dateMaxEl.textContent = range.max || "";

  if (allDates.length > 0) {
    sliderEl.min = 0;
    sliderEl.max = allDates.length - 1;
    sliderEl.value = allDates.length - 1;
    selectDateByIndex(allDates.length - 1);
    calendarMonth = new Date(allDates[allDates.length - 1] + "T00:00:00");
  } else {
    sliderEl.min = 0;
    sliderEl.max = 0;
    sliderEl.value = 0;
    dateCurrentEl.textContent = range.today || "No data";
    calendarMonth = new Date();
  }

  updateGlobalHero();
  renderCalendar();
  updateCharts();
}

function updateGlobalHero() {
  const trackedDays = timeline.length;
  const totalFindings = timeline.reduce(function (sum, entry) {
    return sum + (entry.findings || 0);
  }, 0);
  const totalPRs = timeline.reduce(function (sum, entry) {
    return sum + (entry.prs || 0);
  }, 0);
  const busiest = timeline.reduce(function (best, entry) {
    const score = (entry.findings || 0) + (entry.prs || 0) * 2 + (entry.commits || 0);
    if (!best || score > best.score) {
      return { score: score, date: entry.date };
    }
    return best;
  }, null);
  const latest = allDates.length ? allDates[allDates.length - 1] : "";

  heroDaysEl.textContent = String(trackedDays);
  heroDaysNoteEl.textContent = trackedDays
    ? "From " + formatDateShort(allDates[0]) + " to " + formatDateShort(allDates[allDates.length - 1])
    : "No timeline data yet";
  heroFindingsEl.textContent = String(totalFindings);
  heroFindingsNoteEl.textContent = totalFindings
    ? averagePerDay(totalFindings, trackedDays) + " per tracked day"
    : "No findings recorded yet";
  heroPRsEl.textContent = String(totalPRs);
  heroPRsNoteEl.textContent = busiest && busiest.date
    ? "Busiest day: " + formatDateShort(busiest.date)
    : "No implementation activity yet";
  heroLatestEl.textContent = latest ? formatDateShort(latest) : "No data";
  heroLatestNoteEl.textContent = latest ? "Latest run on file" : "Waiting for the first run";
}

function averagePerDay(total, days) {
  if (!days) {
    return "0";
  }
  const avg = total / days;
  return avg >= 10 ? avg.toFixed(0) : avg.toFixed(1);
}

function currentDate() {
  const idx = Number(sliderEl.value);
  return allDates[idx] || "";
}

function selectDateByIndex(idx) {
  if (idx < 0 || idx >= allDates.length) {
    return;
  }
  const date = allDates[idx];
  sliderEl.value = idx;
  dateCurrentEl.textContent = formatDateLong(date);
  send({ type: "select_date", date: date });
  renderCalendar();
}

sliderEl.addEventListener("input", function () {
  selectDateByIndex(Number(this.value));
});

prevDayBtn.addEventListener("click", function () {
  selectDateByIndex(Number(sliderEl.value) - 1);
});

nextDayBtn.addEventListener("click", function () {
  selectDateByIndex(Number(sliderEl.value) + 1);
});

function handleDayDetail(msg) {
  renderRunSummary(msg.run_summary);
  renderPRs(msg.prs || []);
  renderFindings(msg.findings || []);
  renderCommits(msg.commits || []);
  updateCharts();
}

function renderRunSummary(summary) {
  const card = document.getElementById("run-summary-card");
  const content = document.getElementById("run-summary-content");
  clearChildren(content);

  if (!summary) {
    card.classList.add("hidden");
    return;
  }

  card.classList.remove("hidden");

  [
    ["Sources Scraped", summary.sources_scraped],
    ["Findings Total", summary.findings_total],
    ["Relevant Findings", summary.findings_relevant],
    ["PRs Created", summary.prs_created],
    ["Email Sent", summary.email_sent ? "Yes" : "No"],
  ].forEach(function (item) {
    const stat = document.createElement("div");
    stat.className = "summary-stat";

    const label = document.createElement("span");
    label.className = "label";
    label.textContent = item[0];

    const value = document.createElement("span");
    value.className = "value";
    value.textContent = String(item[1]);

    stat.appendChild(label);
    stat.appendChild(value);
    content.appendChild(stat);
  });
}

function renderPRs(prs) {
  const container = document.getElementById("prs-content");
  document.getElementById("prs-count").textContent = String(prs.length);
  clearChildren(container);

  if (!prs.length) {
    container.appendChild(createEmptyState("No PRs linked to this date"));
    return;
  }

  prs.forEach(function (pr) {
    const card = document.createElement("article");
    card.className = "item-card";

    const title = document.createElement("div");
    title.className = "item-title";
    title.textContent = pr.title || "Untitled PR";
    card.appendChild(title);

    const meta = document.createElement("div");
    meta.className = "item-meta";
    meta.textContent =
      [humanizeToken(pr.category), humanizeToken(pr.status), pr.lines ? pr.lines + " lines" : ""]
        .filter(Boolean)
        .join(" | ");
    card.appendChild(meta);

    if (pr.url) {
      const links = document.createElement("div");
      links.className = "finding-links";

      const openLink = document.createElement("a");
      openLink.className = "inline-link";
      openLink.href = pr.url;
      openLink.target = "_blank";
      openLink.rel = "noopener noreferrer";
      openLink.textContent = "Open pull request";
      links.appendChild(openLink);

      card.appendChild(links);
    }

    container.appendChild(card);
  });
}

function renderFindings(findings) {
  const container = document.getElementById("findings-content");
  document.getElementById("findings-count").textContent = String(findings.length);
  clearChildren(container);

  if (!findings.length) {
    container.appendChild(createEmptyState("No findings recorded for this date"));
    return;
  }

  findings.forEach(function (finding) {
    container.appendChild(createFindingCard(finding, true));
  });
}

function renderCommits(commits) {
  const container = document.getElementById("commits-content");
  document.getElementById("commits-count").textContent = String(commits.length);
  clearChildren(container);

  if (!commits.length) {
    container.appendChild(createEmptyState("No commits for this date"));
    return;
  }

  commits.forEach(function (commit) {
    const card = document.createElement("article");
    card.className = "item-card";

    const title = document.createElement("div");
    title.className = "item-title";
    title.textContent = shortSHA(commit.sha) + " " + (commit.message || "");
    card.appendChild(title);

    const meta = document.createElement("div");
    meta.className = "item-meta";
    meta.textContent = commit.timestamp || "";
    card.appendChild(meta);

    container.appendChild(card);
  });
}

function ensureFindingsLoaded() {
  if (findingsLoaded) {
    renderFindingLibrary();
    return;
  }
  findingsSummaryEl.textContent = "Loading findings…";
  send({ type: "list_findings" });
}

function handleFindingsLibrary(msg) {
  findingsLibrary = Array.isArray(msg.findings) ? msg.findings : [];
  findingsLoaded = true;
  populateFindingFilters();
  renderFindingLibrary();
}

function populateFindingFilters() {
  const currentDisposition = findingDispositionEl.value || "all";
  const currentCategory = findingCategoryEl.value || "all";

  const dispositions = uniqueValues(
    findingsLibrary.map(function (finding) {
      return finding.disposition;
    }),
  );
  const categories = uniqueValues(
    findingsLibrary.map(function (finding) {
      return finding.category;
    }),
  );

  populateSelect(findingDispositionEl, "All dispositions", dispositions, currentDisposition);
  populateSelect(findingCategoryEl, "All categories", categories, currentCategory);
}

function uniqueValues(values) {
  return values
    .filter(Boolean)
    .filter(function (value, index, list) {
      return list.indexOf(value) === index;
    })
    .sort();
}

function populateSelect(select, allLabel, values, currentValue) {
  clearChildren(select);

  const allOption = document.createElement("option");
  allOption.value = "all";
  allOption.textContent = allLabel;
  select.appendChild(allOption);

  values.forEach(function (value) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = humanizeToken(value);
    select.appendChild(option);
  });

  select.value = values.indexOf(currentValue) >= 0 ? currentValue : "all";
}

function renderFindingLibrary() {
  const filtered = getFilteredFindings();
  updateFindingHighlights();

  findingsSummaryEl.textContent = findingsLibrary.length
    ? filtered.length +
      " of " +
      findingsLibrary.length +
      " findings shown" +
      (filtered.length !== findingsLibrary.length ? " after filters" : "")
    : "No findings available yet";

  clearChildren(findingsLibraryEl);

  if (!filtered.length) {
    findingsLibraryEl.appendChild(
      createEmptyState("No findings match the current filters. Try widening the search or resetting a dropdown."),
    );
    return;
  }

  filtered.forEach(function (finding) {
    findingsLibraryEl.appendChild(createFindingCard(finding, false));
  });
}

function updateFindingHighlights() {
  const total = findingsLibrary.length;
  const highRelevance = findingsLibrary.filter(function (finding) {
    return Number(finding.relevance || 0) >= 8;
  }).length;
  const proposed = findingsLibrary.filter(function (finding) {
    return finding.disposition === "proposed";
  }).length;
  const latest = findingsLibrary.length ? findingsLibrary[0].date : "";

  findingTotalEl.textContent = String(total);
  findingHighEl.textContent = String(highRelevance);
  findingProposedEl.textContent = String(proposed);
  findingLatestEl.textContent = latest ? formatDateShort(latest) : "No data";
}

function getFilteredFindings() {
  let items = findingsLibrary.slice();
  const query = (findingSearchEl.value || "").trim().toLowerCase();
  const disposition = findingDispositionEl.value;
  const category = findingCategoryEl.value;
  const sortBy = findingSortEl.value;

  if (query) {
    items = items.filter(function (finding) {
      return [
        finding.finding_id,
        finding.title,
        finding.category,
        finding.source_url,
        finding.reasoning,
        finding.disposition,
      ]
        .join(" ")
        .toLowerCase()
        .includes(query);
    });
  }

  if (disposition && disposition !== "all") {
    items = items.filter(function (finding) {
      return finding.disposition === disposition;
    });
  }

  if (category && category !== "all") {
    items = items.filter(function (finding) {
      return finding.category === category;
    });
  }

  return sortFindings(items, sortBy);
}

function sortFindings(items, sortBy) {
  return items.sort(function (a, b) {
    switch (sortBy) {
      case "priority":
        return compareNumbers(b.rank, a.rank) || compareStringsDesc(a.run_id, b.run_id);
      case "impact":
        return compareNumbers(b.impact, a.impact) || compareStringsDesc(a.run_id, b.run_id);
      case "relevance":
        return compareNumbers(b.relevance, a.relevance) || compareStringsDesc(a.run_id, b.run_id);
      case "low_risk":
        return compareNumbers(a.risk, b.risk) || compareNumbers(b.rank, a.rank);
      case "latest":
      default:
        return compareStringsDesc(a.run_id, b.run_id) || compareNumbers(b.rank, a.rank);
    }
  });
}

function compareNumbers(a, b) {
  return Number(a || 0) - Number(b || 0);
}

function compareStringsDesc(a, b) {
  return String(b || "").localeCompare(String(a || ""));
}

function createFindingCard(finding, compact) {
  const card = document.createElement("article");
  card.className = compact ? "finding-card finding-card-compact" : "finding-card";

  const top = document.createElement("div");
  top.className = "finding-top";

  const badgeRow = document.createElement("div");
  badgeRow.className = "pill-row";
  badgeRow.appendChild(
    createPill(
      humanizeToken(finding.category || "uncategorized"),
      "pill pill-category",
      compact
        ? null
        : function () {
            findingCategoryEl.value = finding.category || "all";
            renderFindingLibrary();
          },
    ),
  );
  badgeRow.appendChild(
    createPill(
      humanizeToken(finding.disposition || "unknown"),
      "pill " + dispositionClass(finding.disposition),
    ),
  );
  top.appendChild(badgeRow);

  const date = document.createElement("div");
  date.className = "finding-date";
  date.textContent = finding.date ? formatDateShort(finding.date) : "Unknown date";
  top.appendChild(date);

  card.appendChild(top);

  const title = document.createElement("h3");
  title.className = "finding-title";
  title.textContent = finding.title || "Untitled finding";
  card.appendChild(title);

  const scoreRow = document.createElement("div");
  scoreRow.className = "score-row";
  scoreRow.appendChild(createScoreChip("Relevance", finding.relevance));
  scoreRow.appendChild(createScoreChip("Impact", finding.impact));
  scoreRow.appendChild(createScoreChip("Risk", finding.risk));
  scoreRow.appendChild(createScoreChip("Priority", finding.rank));
  card.appendChild(scoreRow);

  const links = document.createElement("div");
  links.className = "finding-links";

  const sourceText = document.createElement("span");
  sourceText.className = "pill";
  sourceText.textContent = "Source: " + sourceLabel(finding.source_url);
  links.appendChild(sourceText);

  if (isExternalURL(finding.source_url)) {
    const sourceLink = document.createElement("a");
    sourceLink.className = "inline-link";
    sourceLink.href = finding.source_url;
    sourceLink.target = "_blank";
    sourceLink.rel = "noopener noreferrer";
    sourceLink.textContent = "Open source";
    links.appendChild(sourceLink);
  }

  if (isExternalURL(finding.pr_url)) {
    const prLink = document.createElement("a");
    prLink.className = "inline-link";
    prLink.href = finding.pr_url;
    prLink.target = "_blank";
    prLink.rel = "noopener noreferrer";
    prLink.textContent = "Open PR";
    links.appendChild(prLink);
  }

  card.appendChild(links);

  const details = document.createElement("details");
  details.className = "detail-toggle";

  const summary = document.createElement("summary");
  summary.textContent = compact ? "Reasoning and checks" : "Reasoning, review, and audit details";
  details.appendChild(summary);

  const detailBody = document.createElement("div");
  detailBody.className = "detail-body";

  if (finding.reasoning) {
    const reasoning = document.createElement("p");
    reasoning.className = "finding-reasoning";
    reasoning.textContent = finding.reasoning;
    detailBody.appendChild(reasoning);
  }

  const auditList = document.createElement("div");
  auditList.className = "audit-list";
  auditList.appendChild(createAuditRow("Finding ID", finding.finding_id || "—"));
  auditList.appendChild(createAuditRow("Checks", finding.tests_passed ? "Passed" : "Not passed"));
  auditList.appendChild(createAuditRow("License", finding.license_check || "Unknown"));
  auditList.appendChild(createAuditRow("Security Review", finding.security_review || "Not recorded"));
  detailBody.appendChild(auditList);

  details.appendChild(detailBody);
  card.appendChild(details);

  return card;
}

function createPill(text, className, onClick) {
  const pill = document.createElement("span");
  pill.className = className;
  pill.textContent = text;
  if (typeof onClick === "function") {
    pill.addEventListener("click", onClick);
  }
  return pill;
}

function createScoreChip(label, value) {
  const chip = document.createElement("span");
  chip.className = "score-chip";

  const text = document.createElement("span");
  text.textContent = label;
  chip.appendChild(text);

  const strong = document.createElement("strong");
  strong.textContent = value != null ? String(value) : "0";
  chip.appendChild(strong);

  return chip;
}

function createAuditRow(label, value) {
  const row = document.createElement("div");
  row.className = "audit-row";

  const left = document.createElement("span");
  left.textContent = label;
  row.appendChild(left);

  const right = document.createElement("strong");
  right.textContent = value;
  row.appendChild(right);

  return row;
}

function dispositionClass(value) {
  return "pill-disposition-" + String(value || "unknown").toLowerCase().replace(/[^a-z0-9]+/g, "-");
}

findingSearchEl.addEventListener("input", renderFindingLibrary);
findingDispositionEl.addEventListener("change", renderFindingLibrary);
findingCategoryEl.addEventListener("change", renderFindingLibrary);
findingSortEl.addEventListener("change", renderFindingLibrary);

searchBoxEl.addEventListener("input", function () {
  clearTimeout(searchTimeout);
  const query = this.value.trim();

  if (query.length < 2) {
    document.getElementById("search-card").classList.add("hidden");
    return;
  }

  searchTimeout = setTimeout(function () {
    send({ type: "search", query: query, date: currentDate() });
  }, 350);
});

searchBoxEl.addEventListener("keydown", function (event) {
  if (event.key !== "Enter") {
    return;
  }
  clearTimeout(searchTimeout);
  const query = this.value.trim();
  if (query.length >= 2) {
    send({ type: "search", query: query, date: currentDate() });
  }
});

function handleSearchResults(msg) {
  const card = document.getElementById("search-card");
  const content = document.getElementById("search-content");
  const results = Array.isArray(msg.results) ? msg.results : [];

  card.classList.remove("hidden");
  document.getElementById("search-count").textContent = String(results.length);
  clearChildren(content);

  if (!results.length) {
    content.appendChild(createEmptyState('No results for "' + (msg.query || "") + '"'));
    return;
  }

  results.forEach(function (result) {
    const item = document.createElement("article");
    item.className = "search-item";

    const header = document.createElement("div");
    header.className = "search-item-header";

    const wing = document.createElement("span");
    wing.className = "search-wing";
    wing.textContent = result.wing + (result.room ? " / " + result.room : "");
    header.appendChild(wing);

    const similarity = document.createElement("span");
    similarity.className = "search-similarity";
    similarity.textContent = ((result.similarity || 0) * 100).toFixed(1) + "%";
    header.appendChild(similarity);

    item.appendChild(header);

    if (result.source_file) {
      const meta = document.createElement("div");
      meta.className = "item-meta";
      meta.textContent = "Source: " + result.source_file;
      item.appendChild(meta);
    }

    const text = document.createElement("div");
    text.className = "search-text";
    text.textContent = result.text || "";
    item.appendChild(text);

    content.appendChild(item);
  });
}

calPrevBtn.addEventListener("click", function () {
  calendarMonth.setMonth(calendarMonth.getMonth() - 1);
  renderCalendar();
});

calNextBtn.addEventListener("click", function () {
  calendarMonth.setMonth(calendarMonth.getMonth() + 1);
  renderCalendar();
});

function renderCalendar() {
  if (!calendarMonth) {
    return;
  }

  const monthNames = [
    "January",
    "February",
    "March",
    "April",
    "May",
    "June",
    "July",
    "August",
    "September",
    "October",
    "November",
    "December",
  ];

  const year = calendarMonth.getFullYear();
  const month = calendarMonth.getMonth();
  calLabelEl.textContent = monthNames[month] + " " + year;

  const activityMap = {};
  timeline.forEach(function (entry) {
    activityMap[entry.date] = entry.activity_level || 0;
  });

  clearChildren(calGridEl);

  const firstDay = new Date(year, month, 1).getDay();
  const startOffset = (firstDay + 6) % 7;
  const daysInMonth = new Date(year, month + 1, 0).getDate();
  const selected = currentDate();

  for (let i = 0; i < startOffset; i++) {
    const empty = document.createElement("div");
    empty.className = "cal-cell empty level-0";
    calGridEl.appendChild(empty);
  }

  for (let day = 1; day <= daysInMonth; day++) {
    const dateStr = year + "-" + pad2(month + 1) + "-" + pad2(day);
    const level = activityMap[dateStr] || 0;
    const cell = document.createElement("div");
    cell.className = "cal-cell level-" + level;
    if (dateStr === selected) {
      cell.classList.add("selected");
    }
    cell.title = dateStr + " (activity " + level + ")";
    cell.setAttribute("data-date", dateStr);
    cell.addEventListener("click", onCalendarCellClick);
    calGridEl.appendChild(cell);
  }
}

function onCalendarCellClick() {
  const date = this.getAttribute("data-date");
  if (!date) {
    return;
  }

  if (Object.prototype.hasOwnProperty.call(dateIndex, date)) {
    selectDateByIndex(dateIndex[date]);
    return;
  }

  dateCurrentEl.textContent = formatDateLong(date);
  send({ type: "select_date", date: date });
  renderCalendar();
}

function updateCharts() {
  updateBarChart();
  updateDoughnutChart();
}

function updateBarChart() {
  const recent = timeline.slice(-7);
  if (!recent.length) {
    chartBarEl.src = "";
    return;
  }

  const config = {
    type: "bar",
    data: {
      labels: recent.map(function (entry) {
        return entry.date.substring(5);
      }),
      datasets: [
        {
          label: "Findings",
          data: recent.map(function (entry) {
            return entry.findings;
          }),
          backgroundColor: "#6ab1ff",
        },
        {
          label: "PRs",
          data: recent.map(function (entry) {
            return entry.prs;
          }),
          backgroundColor: "#51dfb8",
        },
      ],
    },
    options: {
      plugins: {
        legend: { labels: { color: "#c7d3e5" } },
      },
      scales: {
        x: { ticks: { color: "#95a9c7" }, grid: { color: "#23354e" } },
        y: {
          beginAtZero: true,
          ticks: { color: "#95a9c7" },
          grid: { color: "#23354e" },
        },
      },
    },
  };

  chartBarEl.src =
    "https://quickchart.io/chart?w=640&h=320&bkg=%2307111f&c=" +
    encodeURIComponent(JSON.stringify(config));
}

function updateDoughnutChart() {
  const buckets = {};
  timeline.forEach(function (entry) {
    const key = "Level " + (entry.activity_level || 0);
    buckets[key] = (buckets[key] || 0) + 1;
  });

  const labels = Object.keys(buckets);
  if (!labels.length) {
    chartDonutEl.src = "";
    return;
  }

  const config = {
    type: "doughnut",
    data: {
      labels: labels,
      datasets: [
        {
          data: labels.map(function (label) {
            return buckets[label];
          }),
          backgroundColor: ["#182238", "#284c68", "#3988b8", "#51dfb8"],
        },
      ],
    },
    options: {
      plugins: {
        legend: { labels: { color: "#c7d3e5" } },
      },
    },
  };

  chartDonutEl.src =
    "https://quickchart.io/chart?w=640&h=320&bkg=%2307111f&c=" +
    encodeURIComponent(JSON.stringify(config));
}

document.addEventListener("keydown", function (event) {
  if (isTypingContext()) {
    return;
  }

  if (activeTab === "timeline") {
    if (event.key === "ArrowLeft") {
      prevDayBtn.click();
      event.preventDefault();
    } else if (event.key === "ArrowRight") {
      nextDayBtn.click();
      event.preventDefault();
    } else if (event.key === "/" || event.key === "s") {
      searchBoxEl.focus();
      event.preventDefault();
    }
  }
});

function isTypingContext() {
  const tag = document.activeElement ? document.activeElement.tagName : "";
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}

function handleOpportunities(msg) {
  const opps = msg.opportunities || [];
  const stats = msg.opportunity_stats || {};
  const sources = msg.discovered_sources || [];

  document.getElementById("opp-total").textContent = String(stats.total || 0);
  document.getElementById("opp-new").textContent = String(stats["new"] || 0);
  document.getElementById("opp-drafts").textContent = String(stats.drafts || 0);
  document.getElementById("opp-won").textContent = String(stats.won || 0);
  document.getElementById("opp-revenue").textContent = "$" + Number(stats.revenue || 0).toLocaleString();

  const list = document.getElementById("opp-list");
  clearChildren(list);

  if (!opps.length) {
    list.appendChild(createEmptyState("No opportunities in the pipeline yet. Run vxd-improve to refresh the feed."));
  } else {
    opps.forEach(function (opp) {
      list.appendChild(createOpportunityCard(opp));
    });
  }

  renderDiscoveredSources(sources);

  const revenueSummaryEl = document.getElementById("revenue-summary");
  if (Number(stats.revenue || 0) > 0) {
    revenueSummaryEl.classList.remove("hidden");
    document.getElementById("revenue-content").textContent =
      "Total recorded revenue: $" + Number(stats.revenue || 0).toLocaleString();
  } else {
    revenueSummaryEl.classList.add("hidden");
  }
}

function createOpportunityCard(opp) {
  const card = document.createElement("article");
  card.className = "opp-card";

  const header = document.createElement("div");
  header.className = "opp-card-header";

  const title = document.createElement("span");
  title.className = "opp-card-title";
  title.textContent = opp.title || "(untitled)";
  header.appendChild(title);

  const rank = document.createElement("span");
  rank.className = "opp-card-rank";
  rank.textContent = "Rank " + (opp.rank || 0);
  header.appendChild(rank);

  card.appendChild(header);

  const meta = document.createElement("div");
  meta.className = "opp-card-meta";
  meta.textContent = [
    humanizeToken(opp.source),
    opp.company || "",
    opp.budget || "No budget",
    "R:" + (opp.relevance_score || 0),
    "B:" + (opp.budget_score || 0),
    "W:" + (opp.win_probability || 0),
    humanizeToken(opp.status),
  ]
    .filter(Boolean)
    .join(" | ");
  card.appendChild(meta);

  if (Array.isArray(opp.skills) && opp.skills.length) {
    const skills = document.createElement("div");
    skills.className = "opp-card-skills";
    opp.skills.forEach(function (skill) {
      const tag = document.createElement("span");
      tag.className = "opp-skill-tag";
      tag.textContent = skill;
      skills.appendChild(tag);
    });
    card.appendChild(skills);
  }

  const actions = document.createElement("div");
  actions.className = "opp-card-actions";

  const viewBtn = document.createElement("button");
  viewBtn.className = "opp-btn";
  viewBtn.textContent = "Open listing";
  viewBtn.addEventListener("click", function () {
    if (opp.url) {
      window.open(opp.url, "_blank", "noopener");
    }
  });
  actions.appendChild(viewBtn);

  if (opp.status === "new" || opp.status === "reviewed") {
    const interestedBtn = document.createElement("button");
    interestedBtn.className = "opp-btn";
    interestedBtn.textContent = "Mark interested";
    interestedBtn.addEventListener("click", function () {
      send({ type: "update_opportunity", id: opp.id, status: "interested" });
    });
    actions.appendChild(interestedBtn);
  }

  if (opp.proposal_draft) {
    const viewProposalBtn = document.createElement("button");
    viewProposalBtn.className = "opp-btn opp-btn-gold";
    viewProposalBtn.textContent = "View proposal";
    viewProposalBtn.addEventListener("click", function () {
      const el = card.querySelector(".opp-proposal");
      if (el) {
        el.classList.toggle("visible");
      }
    });
    actions.appendChild(viewProposalBtn);

    const copyBtn = document.createElement("button");
    copyBtn.className = "opp-btn opp-btn-gold";
    copyBtn.textContent = "Copy & open";
    copyBtn.addEventListener("click", function () {
      navigator.clipboard
        .writeText(opp.proposal_draft)
        .then(function () {
          copyBtn.textContent = "Copied";
          setTimeout(function () {
            copyBtn.textContent = "Copy & open";
          }, 1800);
        })
        .catch(function () {
          copyBtn.textContent = "Copy failed";
          setTimeout(function () {
            copyBtn.textContent = "Copy & open";
          }, 1800);
        });
      if (opp.url) {
        window.open(opp.url, "_blank", "noopener");
      }
    });
    actions.appendChild(copyBtn);
  }

  if (opp.status !== "won" && opp.status !== "lost" && opp.status !== "expired") {
    const wonBtn = document.createElement("button");
    wonBtn.className = "opp-btn opp-btn-gold";
    wonBtn.textContent = "Log win";
    wonBtn.addEventListener("click", function () {
      const amount = prompt("Enter amount earned (USD):");
      if (amount && !isNaN(parseFloat(amount))) {
        send({ type: "log_revenue", id: opp.id, amount: parseFloat(amount) });
      }
    });
    actions.appendChild(wonBtn);

    const lostBtn = document.createElement("button");
    lostBtn.className = "opp-btn opp-btn-danger";
    lostBtn.textContent = "Mark lost";
    lostBtn.addEventListener("click", function () {
      send({ type: "update_opportunity", id: opp.id, status: "lost" });
    });
    actions.appendChild(lostBtn);
  }

  card.appendChild(actions);

  if (opp.proposal_draft) {
    const proposal = document.createElement("div");
    proposal.className = "opp-proposal";
    proposal.textContent = opp.proposal_draft;
    card.appendChild(proposal);
  }

  return card;
}

function renderDiscoveredSources(sources) {
  const pending = sources.filter(function (source) {
    return source.status === "pending" || source.status === "pending_approval";
  });
  const card = document.getElementById("discovered-sources-card");
  const content = document.getElementById("discovered-sources-content");

  if (!pending.length) {
    card.classList.add("hidden");
    clearChildren(content);
    return;
  }

  card.classList.remove("hidden");
  clearChildren(content);

  pending.forEach(function (source) {
    const item = document.createElement("article");
    item.className = "source-card";

    const title = document.createElement("div");
    title.className = "item-title";
    title.textContent = source.name || source.url || "Unnamed source";
    item.appendChild(title);

    const meta = document.createElement("div");
    meta.className = "item-meta";
    meta.textContent = [source.reason, source.discovered_on ? "Discovered " + source.discovered_on : ""]
      .filter(Boolean)
      .join(" | ");
    item.appendChild(meta);

    const approveBtn = document.createElement("button");
    approveBtn.className = "opp-btn";
    approveBtn.style.marginTop = "12px";
    approveBtn.textContent = "Approve source";
    approveBtn.addEventListener("click", function () {
      send({ type: "approve_source", url: source.url });
    });
    item.appendChild(approveBtn);

    content.appendChild(item);
  });
}

function handleRevenueUpdate(msg) {
  if (msg.milestone) {
    alert("Mission Milestone Reached: " + msg.milestone + "!");
  }
  send({ type: "list_opportunities", filter: "all", sort: "rank" });
}

function handleProposalReady() {
  send({ type: "list_opportunities", filter: "all", sort: "rank" });
}

document.getElementById("opp-filter").addEventListener("change", function () {
  send({ type: "list_opportunities", filter: this.value, sort: "rank" });
});

document.getElementById("opp-refresh").addEventListener("click", function () {
  send({ type: "list_opportunities", filter: document.getElementById("opp-filter").value, sort: "rank" });
});

function createEmptyState(text) {
  const empty = document.createElement("div");
  empty.className = "empty-state";
  empty.textContent = text;
  return empty;
}

function sourceLabel(value) {
  if (!value) {
    return "Unknown";
  }
  if (isExternalURL(value)) {
    try {
      return new URL(value).hostname.replace(/^www\./, "");
    } catch (_) {
      return value;
    }
  }
  return value;
}

function isExternalURL(value) {
  return /^https?:\/\//.test(String(value || ""));
}

function humanizeToken(value) {
  if (!value) {
    return "Unknown";
  }
  return String(value)
    .replace(/[_-]+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/\b\w/g, function (char) {
      return char.toUpperCase();
    });
}

function formatDateLong(dateStr) {
  if (!dateStr) {
    return "Unknown date";
  }
  const date = new Date(dateStr + "T00:00:00");
  if (Number.isNaN(date.getTime())) {
    return dateStr;
  }
  return date.toLocaleDateString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

function formatDateShort(dateStr) {
  if (!dateStr) {
    return "Unknown";
  }
  const date = new Date(dateStr + "T00:00:00");
  if (Number.isNaN(date.getTime())) {
    return dateStr;
  }
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

function shortSHA(value) {
  return value ? String(value).slice(0, 8) : "--------";
}

function pad2(num) {
  return num < 10 ? "0" + num : String(num);
}

activateTab("timeline");
connect();
