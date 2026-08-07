'use strict';

const byId = id => document.getElementById(id);
const token = byId('token');
const result = byId('result');
const connection = byId('connection');
const notice = byId('notice');
let editable = null;
let validatedDigest = '';

function notify(message) {
  notice.textContent = message;
  notice.classList.add('show');
  window.setTimeout(() => notice.classList.remove('show'), 5000);
}

async function call(path, options = {}) {
  const headers = {Authorization: `Bearer ${token.value}`, ...(options.headers || {})};
  if (options.body && !headers['Content-Type']) headers['Content-Type'] = 'application/json';
  const response = await fetch(path, {...options, headers});
  if (!response.ok) {
    let message = response.statusText;
    try { message = (await response.json()).detail || message; } catch (_) { /* response is not JSON */ }
    throw new Error(message);
  }
  return response;
}

function showView(id) {
  document.querySelectorAll('.view').forEach(view => view.classList.toggle('active', view.id === id));
  document.querySelectorAll('.nav-item').forEach(item => item.classList.toggle('active', item.dataset.view === id));
}
document.querySelectorAll('.nav-item').forEach(item => item.addEventListener('click', () => showView(item.dataset.view)));

function markChanged() {
  validatedDigest = '';
  byId('apply').disabled = true;
  updatePreview();
}

function updatePreview() {
  if (editable) byId('candidate').value = JSON.stringify(editable, null, 2);
}

function fillForm() {
  const config = editable.configuration;
  byId('mqttEnabled').checked = config.mqtt.enabled;
  byId('siteId').value = config.mqtt.siteId || '';
  byId('broker').value = config.mqtt.broker || '';
  byId('clientId').value = config.mqtt.clientId || '';
  byId('influxEnabled').checked = config.storage.influxdb.enabled;
  byId('influxUrl').value = config.storage.influxdb.url || '';
  byId('influxOrg').value = config.storage.influxdb.organization || '';
  byId('influxBucket').value = config.storage.influxdb.bucket || '';
  byId('influxToken').value = editable.influxdbTokenFile || '';
  renderInventory();
  updatePreview();
}

function collectServices() {
  const config = editable.configuration;
  config.mqtt.enabled = byId('mqttEnabled').checked;
  config.mqtt.homeAssistant.enabled = config.mqtt.enabled;
  config.mqtt.siteId = byId('siteId').value.trim();
  config.mqtt.broker = byId('broker').value.trim();
  config.mqtt.clientId = byId('clientId').value.trim();
  config.storage.influxdb.enabled = byId('influxEnabled').checked;
  config.storage.influxdb.url = byId('influxUrl').value.trim();
  config.storage.influxdb.organization = byId('influxOrg').value.trim();
  config.storage.influxdb.bucket = byId('influxBucket').value.trim();
  editable.influxdbTokenFile = byId('influxToken').value.trim();
  markChanged();
}

function validShortID(value) { return /^[a-z][a-z0-9-]{0,62}$/.test(value); }

function deviceOptions() {
  return editable.configuration.inventory.devices.map(device => `<option value="${escapeHTML(device.id)}">${escapeHTML(device.id)}</option>`).join('');
}

function escapeHTML(value) {
  const node = document.createElement('span');
  node.textContent = value;
  return node.innerHTML;
}

function renderInventory() {
  const inventory = editable.configuration.inventory;
  byId('sensorDevice').innerHTML = deviceOptions();
  byId('equipmentDevice').innerHTML = deviceOptions();
  const rows = [
    ...inventory.devices.map(value => ({kind: 'Device', id: value.id, detail: 'Physical or network device'})),
    ...inventory.sensors.map(value => ({kind: 'Sensor', id: value.id, detail: `${value.unit} on ${value.deviceId}`})),
    ...inventory.equipment.map(value => ({kind: 'Equipment', id: value.id, detail: `on ${value.deviceId}`}))
  ];
  byId('inventoryList').innerHTML = rows.length ? rows.map(row => `<div class="inventory-row"><div><span class="tag">${row.kind}</span></div><strong>${escapeHTML(row.id)}</strong><span>${escapeHTML(row.detail)}</span></div>`).join('') : '<p class="empty">No equipment or sensors have been added.</p>';
}

function addInventory(kind, value) {
  const id = value.id.trim();
  if (!validShortID(id)) throw new Error('Use lowercase letters, numbers, and hyphens; start with a letter.');
  const inventory = editable.configuration.inventory;
  const allIDs = [...inventory.devices, ...inventory.sensors, ...inventory.equipment].map(item => item.id);
  if (allIDs.includes(id)) throw new Error('That ID is already used. Choose a different one.');
  inventory[kind].push({...value, id});
  renderInventory();
  markChanged();
}

byId('connect').addEventListener('click', async () => {
  try {
    if (!token.value) throw new Error('Enter the administrator access code.');
    const [statusResponse, configResponse] = await Promise.all([call('/api/status'), call('/api/config')]);
    result.textContent = JSON.stringify(await statusResponse.json(), null, 2);
    editable = await configResponse.json();
    fillForm();
    connection.textContent = 'Securely connected';
    connection.className = 'status connected';
    token.setAttribute('readonly', 'readonly');
    notify('Connected. Your current settings were loaded.');
    showView('system');
  } catch (error) { connection.textContent = error.message; }
});

byId('verify').addEventListener('click', async () => {
  try {
    const diagnostics = await (await call('/api/verify', {method: 'POST', body: '{}'})).json();
    byId('checks').innerHTML = Object.entries(diagnostics.checks).map(([name, state]) => `<div class="check ${state}"><strong>${escapeHTML(name)}</strong><div>${state === 'pass' ? 'Passed' : 'Needs attention'}</div></div>`).join('');
    notify('System check finished.');
  } catch (error) { notify(error.message); }
});

byId('saveServices').addEventListener('click', async () => {
  try { collectServices(); await validateCandidate(); notify('Service settings are valid.'); showView('inventory'); } catch (error) { notify(error.message); }
});

byId('deviceForm').addEventListener('submit', event => { event.preventDefault(); try { addInventory('devices', {id: byId('deviceId').value}); event.target.reset(); } catch (error) { notify(error.message); } });
byId('sensorForm').addEventListener('submit', event => { event.preventDefault(); try { addInventory('sensors', {id: byId('sensorId').value, deviceId: byId('sensorDevice').value, unit: byId('sensorUnit').value}); event.target.reset(); } catch (error) { notify(error.message); } });
byId('equipmentForm').addEventListener('submit', event => { event.preventDefault(); try { addInventory('equipment', {id: byId('equipmentId').value, deviceId: byId('equipmentDevice').value}); event.target.reset(); } catch (error) { notify(error.message); } });
byId('checkInventory').addEventListener('click', async () => { try { await validateCandidate(); showView('safety'); } catch (error) { notify(error.message); } });

async function validateCandidate() {
  if (!editable) throw new Error('Connect to AquaOS first.');
  const response = await call('/api/config/editable/validate', {method: 'POST', body: JSON.stringify(editable)});
  const validation = await response.json();
  validatedDigest = validation.digest;
  byId('apply').disabled = !byId('understand').checked;
  updatePreview();
  return validation;
}

byId('understand').addEventListener('change', () => { byId('apply').disabled = !(byId('understand').checked && validatedDigest); });
byId('validate').addEventListener('click', async () => { try { const value = await validateCandidate(); notify(`Configuration passed validation (${value.digest.slice(0, 12)}…).`); } catch (error) { notify(error.message); } });
byId('apply').addEventListener('click', async () => {
  try {
    if (!validatedDigest || !byId('understand').checked) throw new Error('Validate and acknowledge the safety review first.');
    if (!window.confirm('Apply this validated configuration? AquaOS may need to restart before changes take effect.')) return;
    const value = await (await call('/api/config/editable/apply', {method: 'POST', body: JSON.stringify(editable)})).json();
    notify(value.restartRequired ? 'Saved. Restart AquaOS to activate the changes.' : 'Configuration applied.');
    validatedDigest = '';
    byId('apply').disabled = true;
  } catch (error) { notify(error.message); }
});

document.querySelectorAll('[data-action]').forEach(button => button.addEventListener('click', async () => {
  try {
    const action = button.dataset.action;
    if (action === 'backup') {
      const response = await call('/api/backup');
      const link = document.createElement('a');
      link.href = URL.createObjectURL(await response.blob());
      link.download = 'aquaos-backup.zip';
      link.click();
      URL.revokeObjectURL(link.href);
      return;
    }
    const method = action === 'status' ? 'GET' : 'POST';
    const response = await call(`/api/${action}`, {method, body: method === 'POST' ? JSON.stringify({dryRun: true}) : undefined});
    result.textContent = JSON.stringify(await response.json(), null, 2);
  } catch (error) { result.textContent = error.message; }
}));
