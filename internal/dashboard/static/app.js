let charts = {};

function escapeHTML(str) {
  if (str === null || str === undefined) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

async function fetchJSON(url) {
  const res = await fetch(url);
  if (!res.ok) {
    let errMessage = `HTTP ${res.status}`;
    try {
      const errObj = await res.json();
      if (errObj && errObj.error) errMessage = errObj.error;
    } catch (_) {
      try {
        const text = await res.text();
        if (text) errMessage = text.trim();
      } catch (_) {}
    }
    throw new Error(errMessage);
  }
  return res.json();
}

function formatTokens(num) {
  if (num === null || num === undefined) return '0';
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
  return num.toLocaleString();
}

function formatDuration(sec) {
  if (!sec || sec < 0) return '0s';
  if (sec > 0 && sec < 1) return '<1s';
  if (sec < 60) return Math.round(sec) + 's';
  const m = Math.floor(sec / 60);
  const s = Math.round(sec % 60);
  if (m < 60) return s === 0 ? `${m}m` : `${m}m ${s}s`;
  const h = Math.floor(m / 60);
  const remM = m % 60;
  if (h < 24) return remM === 0 ? `${h}h` : `${h}h ${remM}m`;
  const d = Math.floor(h / 24);
  const remH = h % 24;
  return remH === 0 ? `${d}d` : `${d}d ${remH}h`;
}

async function loadDashboard() {
  const period = document.getElementById('period').value;

  const overview = await fetchJSON(`/api/overview?period=${encodeURIComponent(period)}`);
  document.getElementById('total-requests').innerText = (overview.total_requests || 0).toLocaleString();
  document.getElementById('total-tokens').innerText = formatTokens(overview.total_tokens || 0);
  document.getElementById('total-cost').innerText = '$' + (overview.total_cost || 0).toFixed(2);
  document.getElementById('avg-latency').innerText = ((overview.avg_latency_ms || 0) / 1000).toFixed(2) + 's';

  // Cache efficiency & total sessions stat cards
  const hitRate = overview.cache_hit_rate || 0;
  const savings = overview.cache_savings_usd || 0;
  document.getElementById('cache-hit-rate').innerText = hitRate.toFixed(1) + '%';
  document.getElementById('cache-savings').innerText = '$' + savings.toFixed(2) + ' saved';

  const totalSessions = overview.total_sessions || 0;
  const turnsPerSess = overview.avg_turns_per_session || 0;
  document.getElementById('total-sessions').innerText = totalSessions.toLocaleString();
  document.getElementById('turns-per-session').innerText = turnsPerSess.toFixed(1) + ' turns/sess';

  loadWaste(period);
  loadCostOverTime(period);
  loadLatencyOverTime(period);
  loadCostByModel(period);
  loadModelUsage(period);
  loadTokensByModel(period);
  loadToolUsage(period);
  loadToolInvocations(period);
  loadTokenComposition(period);
  loadSessions(period);
  loadRequests(period);
}

async function loadCostOverTime(period) {
  const data = await fetchJSON(`/api/cost-over-time?period=${encodeURIComponent(period)}`);
  const labels = [...new Set(data.map(d => d.date))];
  const costs = labels.map(l => {
    return data.filter(d => d.date === l).reduce((acc, cur) => acc + cur.cost, 0);
  });

  if (charts.costTime) charts.costTime.destroy();
  charts.costTime = new Chart(document.getElementById('costTimeChart'), {
    type: 'line',
    data: {
      labels: labels,
      datasets: [{
        label: 'Cost ($)',
        data: costs,
        borderColor: '#58a6ff',
        tension: 0.2,
        fill: true,
        backgroundColor: 'rgba(88, 166, 255, 0.1)'
      }]
    },
    options: { responsive: true }
  });
}

async function loadLatencyOverTime(period) {
  const data = await fetchJSON(`/api/latency-over-time?period=${encodeURIComponent(period)}`);
  const points = data || [];
  if (charts.latencyTime) charts.latencyTime.destroy();
  charts.latencyTime = new Chart(document.getElementById('latencyTimeChart'), {
    type: 'line',
    data: {
      labels: points.map(d => d.date),
      datasets: [{
        label: 'Avg Latency (s)',
        data: points.map(d => ((d.avg_latency_ms || 0) / 1000).toFixed(2)),
        borderColor: '#f0883e',
        tension: 0.2,
        fill: true,
        backgroundColor: 'rgba(240, 136, 62, 0.1)'
      }]
    },
    options: {
      responsive: true,
      scales: {
        y: {
          beginAtZero: true,
          title: {
            display: true,
            text: 'Seconds'
          }
        }
      }
    }
  });
}

async function loadCostByModel(period) {
  const data = await fetchJSON(`/api/cost-by-model?period=${encodeURIComponent(period)}`);
  if (charts.costModel) charts.costModel.destroy();
  charts.costModel = new Chart(document.getElementById('costModelChart'), {
    type: 'bar',
    data: {
      labels: data.map(d => d.model),
      datasets: [{
        label: 'Cost ($)',
        data: data.map(d => d.cost),
        backgroundColor: '#3fb950'
      }]
    },
    options: { indexAxis: 'y', responsive: true }
  });
}

async function loadModelUsage(period) {
  const data = await fetchJSON(`/api/model-usage?period=${encodeURIComponent(period)}`);
  const points = data || [];
  if (charts.modelUsage) charts.modelUsage.destroy();
  const colors = ['#58a6ff', '#3fb950', '#d29922', '#db61a2', '#a371f7', '#f85149', '#79c0ff', '#56d364', '#e3b341', '#f0883e'];
  charts.modelUsage = new Chart(document.getElementById('modelUsageChart'), {
    type: 'doughnut',
    data: {
      labels: points.map(d => `${d.model} (${(d.percentage || 0).toFixed(1)}%)`),
      datasets: [{
        data: points.map(d => d.request_count),
        backgroundColor: colors.slice(0, points.length)
      }]
    },
    options: { responsive: true }
  });
}

async function loadTokensByModel(period) {
  const data = await fetchJSON(`/api/tokens-by-model?period=${encodeURIComponent(period)}`);
  if (charts.tokensModel) charts.tokensModel.destroy();
  charts.tokensModel = new Chart(document.getElementById('tokensModelChart'), {
    type: 'bar',
    data: {
      labels: data.map(d => d.model),
      datasets: [
        { label: 'Input Tokens', data: data.map(d => d.input_tokens), backgroundColor: '#1f6feb' },
        { label: 'Output Tokens', data: data.map(d => d.output_tokens), backgroundColor: '#a371f7' }
      ]
    },
    options: { responsive: true, scales: { x: { stacked: true }, y: { stacked: true } } }
  });
}

async function loadToolUsage(period) {
  const data = await fetchJSON(`/api/tool-usage?period=${encodeURIComponent(period)}`);
  if (charts.toolUsage) charts.toolUsage.destroy();
  charts.toolUsage = new Chart(document.getElementById('toolUsageChart'), {
    type: 'doughnut',
    data: {
      labels: data.map(d => d.tool),
      datasets: [{
        data: data.map(d => d.request_count),
        backgroundColor: ['#58a6ff', '#3fb950', '#d29922', '#db61a2', '#a371f7']
      }]
    },
    options: { responsive: true }
  });
}

async function loadToolInvocations(period) {
  const data = await fetchJSON(`/api/analytics/tools?period=${encodeURIComponent(period)}`);
  if (charts.toolInvocations) charts.toolInvocations.destroy();

  const labels = data && data.length > 0 ? data.map(d => d.tool_name) : ['None'];
  const values = data && data.length > 0 ? data.map(d => d.total_calls) : [0];
  const colors = ['#58a6ff', '#3fb950', '#d29922', '#db61a2', '#a371f7', '#f85149', '#79c0ff', '#56d364', '#e3b341', '#f0883e'];

  charts.toolInvocations = new Chart(document.getElementById('toolInvocationsChart'), {
    type: 'doughnut',
    data: {
      labels: labels,
      datasets: [{
        data: values,
        backgroundColor: colors.slice(0, labels.length)
      }]
    },
    options: { responsive: true }
  });
}

async function loadTokenComposition(period) {
  const data = await fetchJSON(`/api/analytics/cache?period=${encodeURIComponent(period)}`);
  if (charts.tokenComposition) charts.tokenComposition.destroy();

  const miss = data.cache_miss_tokens || 0;
  const hit = data.cache_hit_tokens || 0;
  const output = data.output_tokens || 0;

  charts.tokenComposition = new Chart(document.getElementById('tokenCompositionChart'), {
    type: 'doughnut',
    data: {
      labels: ['Prompt Miss', 'Cache Hit', 'Output'],
      datasets: [{
        data: [miss, hit, output],
        backgroundColor: ['#d29922', '#3fb950', '#a371f7']
      }]
    },
    options: { responsive: true }
  });
}

async function loadSessions(period) {
  const url = `/api/sessions?period=${encodeURIComponent(period)}&limit=50`;
  const sessions = await fetchJSON(url);
  const tbody = document.getElementById('sessions-table');
  tbody.innerHTML = '';

  if (!sessions || sessions.length === 0) {
    const tr = document.createElement('tr');
    tr.innerHTML = '<td colspan="8" style="text-align: center; color: #8b949e;">No sessions found</td>';
    tbody.appendChild(tr);
    return;
  }

  sessions.forEach(s => {
    const tr = document.createElement('tr');
    const sid = s.session_id || '';
    const sidDisplay = sid.length > 14 ? sid.substring(0, 12) + '…' : sid;
    const duration = formatDuration(s.duration_seconds);
    const cacheRate = (s.cache_hit_rate || 0).toFixed(1) + '%';
    const cost = '$' + (s.total_cost || 0).toFixed(4);
    let tools = `${s.tool_call_count || 0}`;
    if (s.tool_error_count > 0) {
      tools += ` <span style="color: #f85149">(${s.tool_error_count} err)</span>`;
    }

    tr.innerHTML = `
      <td><code title="${escapeHTML(sid)}">${escapeHTML(sidDisplay)}</code></td>
      <td>${escapeHTML(s.tool || '—')}</td>
      <td>${s.turn_count || 0}</td>
      <td>${escapeHTML(duration)}</td>
      <td>${cacheRate}</td>
      <td>${cost}</td>
      <td>${tools}</td>
      <td><button class="btn btn-sm btn-drilldown">Drilldown</button></td>
    `;
    const btn = tr.querySelector('.btn-drilldown');
    if (btn) {
      btn.addEventListener('click', () => showSessionDetail(sid));
    }
    tbody.appendChild(tr);
  });
}

async function loadRequests(period) {
  const url = period ? `/api/requests?period=${encodeURIComponent(period)}` : '/api/requests';
  const reqs = await fetchJSON(url);
  const tbody = document.getElementById('requests-table');
  tbody.innerHTML = '';
  reqs.forEach(r => {
    const tr = document.createElement('tr');
    const cost = r.cost_usd ? '$' + r.cost_usd.toFixed(4) : '—';
    const latency = r.latency_ms ? (r.latency_ms / 1000).toFixed(2) + 's' : '—';
    tr.innerHTML = `
      <td>${new Date(r.timestamp).toLocaleTimeString()}</td>
      <td>${escapeHTML(r.tool)}</td>
      <td>${escapeHTML(r.model || '—')}</td>
      <td>${(r.input_tokens || 0).toLocaleString()}</td>
      <td>${(r.output_tokens || 0).toLocaleString()}</td>
      <td>${cost}</td>
      <td>${latency}</td>
    `;
    tbody.appendChild(tr);
  });
}

async function loadWaste(period) {
  try {
    const waste = await fetchJSON(`/api/waste?period=${encodeURIComponent(period)}`);
    const wasteCostEl = document.getElementById('avoidable-waste');
    if (wasteCostEl) {
      wasteCostEl.innerText = '$' + (waste.total_wasted_usd || 0).toFixed(2);
    }
    const wasteTokEl = document.getElementById('avoidable-tokens');
    if (wasteTokEl) {
      wasteTokEl.innerText = formatTokens(waste.total_wasted_tokens || 0) + ' wasted tokens';
    }
    const healthEl = document.getElementById('avg-health');
    if (healthEl) {
      const score = Math.round(waste.avg_health_score !== undefined ? waste.avg_health_score : 100);
      healthEl.innerText = score + '/100';
      healthEl.className = 'metric ' + (score >= 90 ? 'health-good' : score >= 70 ? 'health-warn' : 'health-bad');
    }
    const flaggedEl = document.getElementById('flagged-sessions');
    if (flaggedEl) {
      flaggedEl.innerText = `${waste.wasted_sessions || 0} flagged sessions`;
    }

    // Render Category Breakdown Chart
    const catCanvas = document.getElementById('wasteCategoryChart');
    if (catCanvas) {
      const totals = waste.category_totals_usd || {};
      const snowball = totals['context_snowball'] || 0;
      const churn = totals['cache_churn'] || 0;
      const errors = totals['tool_error_loop'] || 0;
      const read = totals['repetitive_read'] || 0;

      if (charts.wasteCategory) charts.wasteCategory.destroy();
      charts.wasteCategory = new Chart(catCanvas, {
        type: 'bar',
        data: {
          labels: ['Context Snowball', 'Cache Churn', 'Tool Error Loop', 'Repetitive Read'],
          datasets: [{
            label: 'Waste Cost ($)',
            data: [snowball, churn, errors, read],
            backgroundColor: ['#f85149', '#f0883e', '#d29922', '#a371f7'],
            borderRadius: 4
          }]
        },
        options: {
          responsive: true,
          plugins: {
            legend: { display: false }
          },
          scales: {
            y: {
              beginAtZero: true,
              ticks: {
                callback: function(v) { return '$' + Number(v).toFixed(2); }
              }
            }
          }
        }
      });
    }
  } catch (err) {
    console.error('Failed to load waste analytics:', err);
  }
}

function formatCategoryLabel(cat) {
  switch (cat) {
    case 'context_snowball': return 'Context Snowball';
    case 'cache_churn': return 'Cache Churn';
    case 'tool_error_loop': return 'Tool Error Loop';
    case 'repetitive_read': return 'Repetitive Read';
    default: return cat || 'Waste Finding';
  }
}

async function showSessionDetail(rawSessionId) {
  const sessionId = decodeURIComponent(rawSessionId);
  const modal = document.getElementById('session-modal');
  modal.style.display = 'flex';

  document.getElementById('modal-title').textContent = `Session: ${sessionId}`;
  const summaryEl = document.getElementById('modal-summary');
  summaryEl.innerHTML = '<div style="color: #8b949e">Loading session details...</div>';
  const turnsEl = document.getElementById('modal-turns-table');
  turnsEl.innerHTML = '';
  const healthTagEl = document.getElementById('modal-session-health');
  if (healthTagEl) healthTagEl.innerHTML = '';
  const findingsEl = document.getElementById('modal-waste-findings');
  if (findingsEl) findingsEl.innerHTML = '<div style="color: #8b949e">Analyzing context growth &amp; waste patterns...</div>';

  if (charts.sessionTrajectory) {
    charts.sessionTrajectory.destroy();
    charts.sessionTrajectory = null;
  }

  try {
    const [detail, waste] = await Promise.all([
      fetchJSON(`/api/sessions/detail?id=${encodeURIComponent(sessionId)}`),
      fetchJSON(`/api/sessions/${encodeURIComponent(sessionId)}/waste`).catch(err => {
        console.warn('Could not load session waste analysis:', err);
        return null;
      })
    ]);

    const sm = detail.summary;
    const healthScore = waste && waste.health_score !== undefined ? waste.health_score : 100;
    const healthClass = healthScore >= 90 ? 'health-good' : (healthScore >= 70 ? 'health-warn' : 'health-bad');
    const wastedUSD = waste ? (waste.wasted_usd || 0) : 0;
    const wastePct = waste ? (waste.waste_percentage || 0) : 0;

    summaryEl.innerHTML = `
      <div class="summary-item">
        <span>Tool</span>
        <strong>${escapeHTML(sm.tool || '—')}</strong>
      </div>
      <div class="summary-item">
        <span>Turns</span>
        <strong>${sm.turn_count || 0}</strong>
      </div>
      <div class="summary-item">
        <span>Duration</span>
        <strong>${escapeHTML(formatDuration(sm.duration_seconds))}</strong>
      </div>
      <div class="summary-item">
        <span>Total Tokens</span>
        <strong>${formatTokens(sm.total_tokens || 0)}</strong>
      </div>
      <div class="summary-item">
        <span>Cache Hit Rate</span>
        <strong>${(sm.cache_hit_rate || 0).toFixed(1)}%</strong>
      </div>
      <div class="summary-item">
        <span>Total Cost</span>
        <strong>$${(sm.total_cost || 0).toFixed(4)}</strong>
      </div>
      <div class="summary-item">
        <span>Tool Calls</span>
        <strong>${sm.tool_call_count || 0}${sm.tool_error_count > 0 ? ` (${sm.tool_error_count} err)` : ''}</strong>
      </div>
      <div class="summary-item">
        <span>Health Score</span>
        <strong class="${healthClass}">${healthScore}/100</strong>
      </div>
      <div class="summary-item">
        <span>Avoidable Waste</span>
        <strong style="${wastedUSD > 0 ? 'color: #f85149' : ''}">$${wastedUSD.toFixed(4)}${wastePct > 0 ? ` (${wastePct.toFixed(1)}%)` : ''}</strong>
      </div>
    `;

    if (healthTagEl) {
      healthTagEl.innerHTML = `<span class="health-badge ${healthClass}">Health: ${healthScore}/100</span>`;
    }

    // Render Trajectory Chart
    const trajectoryCanvas = document.getElementById('sessionTrajectoryChart');
    const trajData = (waste && waste.trajectory && waste.trajectory.length > 0)
      ? waste.trajectory
      : (detail.turns || []).map((t, idx) => ({
          step_index: t.step_index > 0 ? t.step_index : (idx + 1),
          input_tokens: t.input_tokens || 0,
          output_tokens: t.output_tokens || 0,
          cache_read_tokens: t.cache_read_tokens || 0,
          cost_usd: t.cost_usd || 0,
          has_tool_error: t.tool_error_count > 0,
          is_waste: false
        }));

    if (trajectoryCanvas && trajData.length > 0) {
      const labels = trajData.map(t => 'Turn ' + t.step_index);
      const inputTokens = trajData.map(t => t.input_tokens);
      const pointBgColors = trajData.map(t => t.is_waste ? '#f85149' : (t.has_tool_error ? '#d29922' : '#58a6ff'));
      const pointBorderColors = trajData.map(t => t.is_waste ? '#ff7b72' : (t.has_tool_error ? '#e3b341' : '#79c0ff'));
      const pointRadii = trajData.map(t => t.is_waste ? 6 : (t.has_tool_error ? 5 : 3.5));
      const pointHoverRadii = trajData.map(t => t.is_waste ? 8 : (t.has_tool_error ? 7 : 5.5));

      charts.sessionTrajectory = new Chart(trajectoryCanvas, {
        type: 'line',
        data: {
          labels: labels,
          datasets: [{
            label: 'Input Tokens (Context Size)',
            data: inputTokens,
            borderColor: '#58a6ff',
            backgroundColor: 'rgba(88, 166, 255, 0.08)',
            fill: true,
            tension: 0.2,
            pointBackgroundColor: pointBgColors,
            pointBorderColor: pointBorderColors,
            pointRadius: pointRadii,
            pointHoverRadius: pointHoverRadii,
            borderWidth: 2
          }]
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          plugins: {
            tooltip: {
              callbacks: {
                label: function(ctx) {
                  const item = trajData[ctx.dataIndex];
                  const lines = [
                    `Input Tokens: ${formatTokens(item.input_tokens)}`,
                    `Cache Read: ${formatTokens(item.cache_read_tokens)}`,
                    `Cost: $${(item.cost_usd || 0).toFixed(4)}`
                  ];
                  if (item.has_tool_error) {
                    lines.push('⚠️ Tool Error detected');
                  }
                  if (item.is_waste) {
                    lines.push('🚨 Flagged as Token Waste');
                  }
                  return lines;
                }
              }
            }
          },
          scales: {
            y: {
              beginAtZero: true,
              title: {
                display: true,
                text: 'Input Tokens'
              },
              ticks: {
                callback: function(v) { return formatTokens(v); }
              }
            },
            x: {
              title: {
                display: true,
                text: 'Session Turn'
              }
            }
          }
        }
      });
    }

    // Render Waste Findings
    if (findingsEl) {
      if (waste && waste.findings && waste.findings.length > 0) {
        let findingsHtml = '<div class="findings-list">';
        waste.findings.forEach(f => {
          const sevClass = `sev-${f.severity || 'low'}`;
          const catLabel = formatCategoryLabel(f.category);
          findingsHtml += `
            <div class="finding-card ${sevClass}">
              <div class="finding-header">
                <span class="finding-cat">${escapeHTML(catLabel)}</span>
                <span class="badge badge-sev badge-sev-${f.severity || 'low'}">${escapeHTML((f.severity || 'low').toUpperCase())}</span>
                <span class="finding-metrics">Turn #${f.turn_index} &bull; ${formatTokens(f.wasted_tokens)} tok &bull; $${(f.wasted_usd || 0).toFixed(4)}</span>
              </div>
              <div class="finding-body">
                <p class="finding-expl">${escapeHTML(f.explanation)}</p>
                ${f.recommendation ? `<p class="finding-rec">💡 <strong>Recommendation:</strong> ${escapeHTML(f.recommendation)}</p>` : ''}
              </div>
            </div>
          `;
        });
        findingsHtml += '</div>';
        findingsEl.innerHTML = findingsHtml;
      } else {
        findingsEl.innerHTML = `
          <div class="finding-empty">
            <span class="icon-check">✅</span> No token waste detected. Context growth and tool interactions are within healthy thresholds.
          </div>
        `;
      }
    }

    // Render Turns Table
    turnsEl.innerHTML = '';
    if (!detail.turns || detail.turns.length === 0) {
      turnsEl.innerHTML = '<tr><td colspan="10" style="text-align: center; color: #8b949e">No turns recorded</td></tr>';
      return;
    }

    const wasteTurnMap = {};
    if (waste && waste.trajectory) {
      waste.trajectory.forEach(tm => {
        wasteTurnMap[tm.step_index] = tm.is_waste;
      });
    }

    detail.turns.forEach((t, idx) => {
      const tr = document.createElement('tr');
      const stepNum = t.step_index > 0 ? t.step_index : (idx + 1);
      const isWaste = !!wasteTurnMap[stepNum];
      if (isWaste) {
        tr.classList.add('tr-waste');
      }
      const timeStr = new Date(t.timestamp).toLocaleTimeString();
      const cost = t.cost_usd !== null && t.cost_usd !== undefined ? '$' + t.cost_usd.toFixed(4) : '—';
      const latency = t.latency_ms ? (t.latency_ms / 1000).toFixed(2) + 's' : '—';

      let toolHtml = '—';
      if (t.tool_calls && t.tool_calls.length > 0) {
        toolHtml = t.tool_calls.map(tc => {
          const cls = tc.status === 'error' ? 'tag-error' : 'tag-ok';
          const name = escapeHTML(tc.tool_name);
          const status = escapeHTML(tc.status);
          return `<span class="tool-tag ${cls}" title="${name} (${status})">${name}</span>`;
        }).join('');
      } else if (t.tool_call_count > 0) {
        toolHtml = `${t.tool_call_count} call(s)`;
      }

      let wasteBadge = '<span class="badge badge-turn-ok">OK</span>';
      if (isWaste) {
        wasteBadge = '<span class="badge badge-waste">Waste</span>';
      } else if (t.tool_error_count > 0) {
        wasteBadge = '<span class="badge badge-warn">Error</span>';
      }

      tr.innerHTML = `
        <td>${stepNum}</td>
        <td>${timeStr}</td>
        <td>${escapeHTML(t.model || '—')}</td>
        <td>${formatTokens(t.input_tokens)}</td>
        <td>${formatTokens(t.cache_read_tokens)}</td>
        <td>${formatTokens(t.output_tokens)}</td>
        <td>${cost}</td>
        <td>${latency}</td>
        <td>${toolHtml}</td>
        <td>${wasteBadge}</td>
      `;
      turnsEl.appendChild(tr);
    });
  } catch (err) {
    summaryEl.innerHTML = `<div style="color: #f85149">Failed to load details: ${escapeHTML(err.message)}</div>`;
  }
}

async function openSessionModal(sessionId) {
  return showSessionDetail(sessionId);
}

async function fetchDashboardData(period) {
  return loadDashboard(period);
}

function closeSessionModal() {
  document.getElementById('session-modal').style.display = 'none';
}

window.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    closeSessionModal();
  }
});

document.addEventListener('DOMContentLoaded', () => {
  loadDashboard();
  setInterval(loadDashboard, 60000);
});
