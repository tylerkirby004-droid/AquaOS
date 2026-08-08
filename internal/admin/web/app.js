'use strict';

const byId = id => document.getElementById(id);
const token = byId('token');
const result = byId('result');
const connection = byId('connection');
const notice = byId('notice');
const handoff = new URLSearchParams(window.location.hash.slice(1)).get('access_token');
if (handoff) {
  token.value = handoff;
  window.history.replaceState(null, '', window.location.pathname + window.location.search);
}
let editable = null;
let validatedDigest = '';
let discovered = [];
let editingEquipmentID = '';
let editingAlarmID = '';

function normalizeConfiguration() {
  const config = editable.configuration;
  config.inventory ||= {devices: [], sensors: [], equipment: []};
  config.inventory.devices ||= [];
  config.inventory.sensors ||= [];
  config.inventory.equipment ||= [];
  config.alarms ||= {rules: []};
  config.alarms.rules ||= [];
  config.backups ||= {enabled: false, destination: '', retentionDays: 30};
  config.adapters.shelly.endpoints ||= [];
  config.adapters.esp32.endpoints ||= [];
}

function duration(value, optional = true) {
  const text = String(value || '').trim();
  if (!text && optional) return 0;
  const match = /^(\d+(?:\.\d+)?)(ms|s|m|h)$/.exec(text);
  if (!match) throw new Error('Use a duration such as 30s, 5m, or 2h.');
  const factors = {ms: 1e6, s: 1e9, m: 60e9, h: 3600e9};
  return Number(match[1]) * factors[match[2]];
}

function durationText(value) {
  const nanoseconds = Number(value || 0);
  if (!nanoseconds) return '';
  if (nanoseconds % 3600e9 === 0) return `${nanoseconds / 3600e9}h`;
  if (nanoseconds % 60e9 === 0) return `${nanoseconds / 60e9}m`;
  return `${nanoseconds / 1e9}s`;
}

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
  normalizeConfiguration();
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
  byId('backupEnabled').checked = config.backups.enabled;
  byId('backupDestination').value = config.backups.destination || '';
  byId('backupRetention').value = config.backups.retentionDays || 30;
  renderInventory();
  renderAlarms();
  updatePreview();
}

function renderRuntime(report) {
  const health = report.health || report;
  const components = Array.isArray(health.components) ? health.components : [];
  const values = Array.isArray(report.canonicalState?.values) ? report.canonicalState.values : [];
  const stateRows = values.filter(value => value.key?.entityKind === 'sensor' || value.key?.entityKind === 'equipment').map(value => ({name: `${value.key.entityKind}: ${value.key.entityId}`, state: value.quality || 'unknown', message: value.key.attribute || ''}));
  const rows = [...components, ...stateRows];
  byId('runtimeStatus').innerHTML = rows.length ? rows.map(component => `<div class="check ${component.state === 'healthy' || component.state === 'good' ? 'pass' : 'fail'}"><strong>${escapeHTML(component.name)}</strong><div>${escapeHTML(component.state || 'unknown')}${component.message ? ` — ${escapeHTML(component.message)}` : ''}</div></div>`).join('') : '<p class="empty">Core returned no component or canonical-state status.</p>';
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
  byId('alarmSensor').innerHTML = inventory.sensors.map(sensor => `<option value="${escapeHTML(sensor.id)}">${escapeHTML(sensor.name || sensor.id)}</option>`).join('');
  const rows = [
    ...inventory.devices.map(value => ({kind: 'Device', id: value.id, detail: value.name || 'Physical or network device'})),
    ...inventory.sensors.map(value => ({kind: 'Sensor', id: value.id, detail: `${value.name || value.id} — ${value.unit} on ${value.deviceId}`, extra: `<button data-calibrate="${escapeHTML(value.id)}">Calibrate</button>`})),
    ...inventory.equipment.map(value => ({kind: 'Equipment', id: value.id, detail: `${value.name || value.id} — ${value.kind || 'unassigned'} — ${(value.commissioning && value.commissioning.stage) || 'uncommissioned'}`, extra: `<button data-commission="${escapeHTML(value.id)}">Commission</button>`}))
  ];
  byId('inventoryList').innerHTML = rows.length ? rows.map(row => `<div class="inventory-row"><div><span class="tag">${row.kind}</span></div><strong>${escapeHTML(row.id)}</strong><span>${escapeHTML(row.detail)}</span><div class="row-actions"><button data-edit-kind="${row.kind.toLowerCase()}" data-edit-id="${escapeHTML(row.id)}">Rename</button>${row.extra || ''}<button data-remove-kind="${row.kind.toLowerCase()}" data-remove-id="${escapeHTML(row.id)}">Remove</button></div></div>`).join('') : '<p class="empty">No equipment or sensors have been added.</p>';
}

function renderAlarms() {
  const rules = editable.configuration.alarms.rules;
  byId('alarmList').innerHTML = rules.length ? rules.map(rule => `<div class="inventory-row"><span class="tag">${escapeHTML(rule.severity)}</span><strong>${escapeHTML(rule.name)}</strong><span>${escapeHTML(rule.sensorId)} ${escapeHTML(rule.condition)} ${rule.threshold}</span><div class="row-actions"><button data-edit-alarm="${escapeHTML(rule.id)}">Edit</button><button data-remove-alarm="${escapeHTML(rule.id)}">Remove</button></div></div>`).join('') : '<p class="empty">No alarm rules configured.</p>';
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
    try { renderRuntime(await (await call('/api/runtime')).json()); } catch (error) { byId('runtimeStatus').innerHTML = `<div class="check fail"><strong>Core status unavailable</strong><div>${escapeHTML(error.message)}</div></div>`; }
    fillForm();
    connection.textContent = 'Securely connected';
    connection.className = 'status connected';
    token.setAttribute('readonly', 'readonly');
    notify('Connected. Your current settings were loaded.');
    showView('system');
  } catch (error) { connection.textContent = error.message; }
});

if (handoff) byId('connect').click();

byId('verify').addEventListener('click', async () => {
  try {
    const [diagnostics, runtime] = await Promise.all([(await call('/api/verify', {method: 'POST', body: '{}'})).json(), (await call('/api/runtime')).json()]);
    renderRuntime(runtime);
    byId('checks').innerHTML = Object.entries(diagnostics.checks).map(([name, state]) => `<div class="check ${state}"><strong>${escapeHTML(name)}</strong><div>${state === 'pass' ? 'Passed' : 'Needs attention'}</div></div>`).join('');
    notify('System check finished.');
  } catch (error) { notify(error.message); }
});

byId('saveServices').addEventListener('click', async () => {
  try { collectServices(); await validateCandidate(); notify('Service settings are valid.'); showView('discovery'); } catch (error) { notify(error.message); }
});

byId('discoveryForm').addEventListener('submit', async event => {
  event.preventDefault();
  try {
    const candidate = {kind: byId('discoveryKind').value, baseUrl: byId('discoveryUrl').value.trim(), channel: Number(byId('discoveryChannel').value), bearerTokenFile: byId('discoveryTokenFile').value.trim()};
    discovered = await (await call('/api/discovery/probe', {method: 'POST', body: JSON.stringify({candidates: [candidate]})})).json();
    byId('discoveryResults').innerHTML = discovered.map((item, index) => `<div class="inventory-row"><span class="tag">${escapeHTML(item.kind)}</span><strong>${escapeHTML(item.identity || item.baseUrl)}</strong><span>${escapeHTML(item.reachable ? item.message || 'Ready to map' : item.message || 'Not reachable')}</span>${item.reachable ? `<button data-map-discovery="${index}">Add to AquaOS</button>` : ''}</div>`).join('');
  } catch (error) { notify(error.message); }
});

function shortID(value, fallback) {
  const normalized = String(value || fallback).toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
  return validShortID(normalized) ? normalized : `${fallback}-${Date.now()}`;
}

function mapDiscovery(item) {
  const config = editable.configuration;
  if (item.kind === 'shelly') {
    const endpointID = crypto.randomUUID();
    const equipmentID = crypto.randomUUID();
    const key = shortID(item.identity, 'shelly-outlet');
    if (config.inventory.devices.some(device => device.id === key)) throw new Error('This Shelly appears to be mapped already.');
    config.inventory.devices.push({id: key, entityId: endpointID, name: `Shelly ${item.identity}`, manufacturer: 'Shelly', metadata: {adapter: 'shelly'}});
    config.inventory.equipment.push({id: `${key}-equipment`, entityId: equipmentID, deviceId: key, name: 'Unassigned outlet', kind: 'outlet', capabilities: ['switch', 'command-acknowledgement', 'reported-state', 'power-telemetry'], hazardous: false, failSafeOn: false, maximumOn: 0, maximumDailyOn: 0, minimumOff: 0, commissioning: {stage: 'discovered'}});
    config.adapters.shelly.endpoints.push({id: endpointID, equipmentId: equipmentID, alarmRuleId: crypto.randomUUID(), baseUrl: item.baseUrl, channel: item.channel || 0, pollInterval: 5e9, requestTimeout: 2e9, retries: 1, safeOn: false, powerReturnPolicy: 'off', equipmentKind: 'outlet', maximumOn: 0, requiredProbeIds: []});
  } else {
    const endpointID = crypto.randomUUID();
    const deviceEntityID = crypto.randomUUID();
    const key = shortID(item.identity, 'esp32-node');
    if (!Array.isArray(item.probeIds) || item.probeIds.length !== 2) throw new Error('AquaOS ESP32 nodes must report exactly two probe UUIDs.');
    item.probeIds.forEach(id => { if (!/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(id)) throw new Error('ESP32 probe identities must be UUIDs.'); });
    config.inventory.devices.push({id: key, entityId: deviceEntityID, name: item.identity || 'ESP32 sensor node', manufacturer: 'AquaOS', firmware: item.firmware || '', metadata: {adapter: 'esp32'}});
    item.probeIds.forEach((entityId, index) => config.inventory.sensors.push({id: `${key}-probe-${index + 1}`, entityId, deviceId: key, name: `Temperature probe ${index + 1}`, quantity: 'temperature', unit: 'celsius', calibration: {enabled: false, scale: 1, offset: 0}}));
    config.adapters.esp32.endpoints.push({id: endpointID, deviceId: deviceEntityID, alarmRuleId: crypto.randomUUID(), baseUrl: item.baseUrl, bearerTokenFile: item.bearerTokenFile || '', probeIds: item.probeIds, pollInterval: 5e9, requestTimeout: 2e9, freshFor: 15e9, maximumClockSkew: 5e9, maximumDifferenceCelsius: 1});
  }
  renderInventory();
  markChanged();
  notify('Device mapped but still disabled. Complete safety commissioning before activation.');
  showView('inventory');
}

byId('deviceForm').addEventListener('submit', event => { event.preventDefault(); try { addInventory('devices', {id: byId('deviceId').value}); event.target.reset(); } catch (error) { notify(error.message); } });
byId('sensorForm').addEventListener('submit', event => { event.preventDefault(); try { const calibrated = byId('calibrationEnabled').checked; addInventory('sensors', {id: byId('sensorId').value, entityId: crypto.randomUUID(), name: byId('sensorName').value.trim(), deviceId: byId('sensorDevice').value, quantity: byId('sensorUnit').value === 'boolean' ? 'boolean' : 'measurement', unit: byId('sensorUnit').value, calibration: {enabled: calibrated, scale: Number(byId('calibrationScale').value), offset: Number(byId('calibrationOffset').value), reference: calibrated ? byId('calibrationReference').value.trim() : '', calibratedBy: calibrated ? 'admin-gui-operator' : '', calibratedAt: calibrated ? new Date().toISOString() : '0001-01-01T00:00:00Z'}}); event.target.reset(); renderAlarms(); } catch (error) { notify(error.message); } });
byId('equipmentForm').addEventListener('submit', event => { event.preventDefault(); try { const kind = byId('equipmentKind').value; const hazardous = ['heater', 'ato', 'dosing-pump'].includes(kind); const requiredSensorIds = byId('requiredSensors').value.split(',').map(value => value.trim()).filter(Boolean); const existing = editingEquipmentID ? editable.configuration.inventory.equipment.find(value => value.id === editingEquipmentID) : null; const value = {id: byId('equipmentId').value.trim(), entityId: existing?.entityId || crypto.randomUUID(), name: byId('equipmentName').value.trim(), deviceId: byId('equipmentDevice').value, kind, capabilities: ['switch', 'command-acknowledgement', 'reported-state'], hazardous, failSafeOn: false, maximumOn: duration(byId('maximumOn').value, !hazardous), maximumDailyOn: duration(byId('maximumDaily').value, kind !== 'dosing-pump'), minimumOff: duration(byId('minimumOff').value), requiredSensorIds, commissioning: existing?.commissioning || {stage: 'uncommissioned'}}; if (existing) { const index = editable.configuration.inventory.equipment.indexOf(existing); editable.configuration.inventory.equipment[index] = value; const endpoint = editable.configuration.adapters.shelly.endpoints.find(item => item.equipmentId === value.entityId); if (endpoint) { endpoint.equipmentKind = kind === 'heater' ? 'heater' : 'outlet'; endpoint.maximumOn = value.maximumOn; endpoint.requiredProbeIds = requiredSensorIds.map(sensorID => editable.configuration.inventory.sensors.find(sensor => sensor.id === sensorID)?.entityId).filter(Boolean); } editingEquipmentID = ''; event.submitter.textContent = 'Add equipment'; renderInventory(); markChanged(); } else { addInventory('equipment', value); } event.target.reset(); } catch (error) { notify(error.message); } });
byId('checkInventory').addEventListener('click', async () => { try { await validateCandidate(); showView('safety'); } catch (error) { notify(error.message); } });

byId('alarmForm').addEventListener('submit', event => {
  event.preventDefault();
  try {
    const id = byId('alarmId').value.trim();
    if (!validShortID(id) || editable.configuration.alarms.rules.some(rule => rule.id === id && rule.id !== editingAlarmID)) throw new Error('Choose a unique lowercase alarm ID.');
    const condition = byId('alarmCondition').value;
    const highText = byId('alarmThresholdHigh').value.trim();
    const notifications = [];
    if (byId('notifyHA').checked) notifications.push('home-assistant');
    if (byId('notifyLog').checked) notifications.push('log');
    const value = {id, name: byId('alarmName').value.trim(), sensorId: byId('alarmSensor').value, condition, threshold: Number(byId('alarmThreshold').value), ...(condition === 'outside' ? {thresholdHigh: Number(highText)} : {}), severity: byId('alarmSeverity').value, delay: duration(byId('alarmDelay').value, false), clearDelay: duration(byId('alarmClearDelay').value, false), latching: byId('alarmLatching').checked, notifications};
    if (editingAlarmID) { const index = editable.configuration.alarms.rules.findIndex(rule => rule.id === editingAlarmID); editable.configuration.alarms.rules[index] = value; editingAlarmID = ''; } else editable.configuration.alarms.rules.push(value);
    renderAlarms();
    markChanged();
    event.target.reset();
  } catch (error) { notify(error.message); }
});

document.addEventListener('click', event => {
  const mapIndex = event.target.dataset.mapDiscovery;
  if (mapIndex !== undefined) {
    try { mapDiscovery(discovered[Number(mapIndex)]); } catch (error) { notify(error.message); }
    return;
  }
  const alarmID = event.target.dataset.removeAlarm;
  if (alarmID) {
    editable.configuration.alarms.rules = editable.configuration.alarms.rules.filter(rule => rule.id !== alarmID);
    renderAlarms(); markChanged(); return;
  }
  const editAlarmID = event.target.dataset.editAlarm;
  if (editAlarmID) {
    const rule = editable.configuration.alarms.rules.find(value => value.id === editAlarmID);
    editingAlarmID = editAlarmID;
    byId('alarmId').value = rule.id; byId('alarmName').value = rule.name; byId('alarmSensor').value = rule.sensorId; byId('alarmCondition').value = rule.condition; byId('alarmThreshold').value = rule.threshold; byId('alarmThresholdHigh').value = rule.thresholdHigh ?? ''; byId('alarmSeverity').value = rule.severity; byId('alarmDelay').value = durationText(rule.delay); byId('alarmClearDelay').value = durationText(rule.clearDelay); byId('alarmLatching').checked = rule.latching; byId('notifyHA').checked = rule.notifications.includes('home-assistant'); byId('notifyLog').checked = rule.notifications.includes('log');
    notify('Edit the alarm form and save it.'); return;
  }
  const editID = event.target.dataset.editId;
  if (editID) {
    const kind = event.target.dataset.editKind;
    if (kind === 'equipment') {
      const item = editable.configuration.inventory.equipment.find(value => value.id === editID);
      editingEquipmentID = editID;
      byId('equipmentId').value = item.id;
      byId('equipmentName').value = item.name || item.id;
      byId('equipmentDevice').value = item.deviceId;
      byId('equipmentKind').value = item.kind || 'outlet';
      byId('requiredSensors').value = (item.requiredSensorIds || []).join(', ');
      byId('maximumOn').value = durationText(item.maximumOn);
      byId('maximumDaily').value = durationText(item.maximumDailyOn);
      byId('minimumOff').value = durationText(item.minimumOff);
      byId('equipmentForm').querySelector('button[type="submit"],button:last-child').textContent = 'Save equipment';
      notify('Edit the equipment form and choose Save equipment.');
      return;
    }
    const collection = kind === 'device' ? 'devices' : kind === 'sensor' ? 'sensors' : 'equipment';
    const item = editable.configuration.inventory[collection].find(value => value.id === editID);
    const name = window.prompt('Display name', item.name || item.id);
    if (name && name.trim()) { item.name = name.trim(); renderInventory(); markChanged(); }
    return;
  }
  const calibrationID = event.target.dataset.calibrate;
  if (calibrationID) {
    const sensor = editable.configuration.inventory.sensors.find(value => value.id === calibrationID);
    byId('calibrationId').value = calibrationID;
    byId('editCalibrationScale').value = sensor.calibration?.scale || 1;
    byId('editCalibrationOffset').value = sensor.calibration?.offset || 0;
    byId('editCalibrationReference').value = sensor.calibration?.reference || '';
    byId('calibrationDialog').showModal(); return;
  }
  const commissionID = event.target.dataset.commission;
  if (commissionID) {
    const equipment = editable.configuration.inventory.equipment.find(value => value.id === commissionID);
    if (equipment.commissioning?.stage === 'bench-tested') {
      if (window.confirm('Authorize this bench-tested equipment for aquarium control?')) { equipment.commissioning.stage = 'commissioned'; renderInventory(); markChanged(); }
      return;
    }
    byId('commissionId').value = commissionID;
    byId('independentSafeguard').checked = !equipment.hazardous;
    byId('commissionDialog').showModal(); return;
  }
  const removeID = event.target.dataset.removeId;
  if (!removeID) return;
  const kind = event.target.dataset.removeKind;
  const inventory = editable.configuration.inventory;
  if (kind === 'device' && (inventory.sensors.some(item => item.deviceId === removeID) || inventory.equipment.some(item => item.deviceId === removeID))) { notify('Remove equipment and sensors assigned to this device first.'); return; }
  if (kind === 'sensor' && editable.configuration.alarms.rules.some(rule => rule.sensorId === removeID)) { notify('Remove alarm rules that use this sensor first.'); return; }
  const collection = kind === 'device' ? 'devices' : kind === 'sensor' ? 'sensors' : 'equipment';
  inventory[collection] = inventory[collection].filter(item => item.id !== removeID);
  renderInventory(); renderAlarms(); markChanged();
});

byId('calibrationForm').addEventListener('submit', event => {
  if (event.submitter?.value === 'cancel') return;
  event.preventDefault();
  const sensor = editable.configuration.inventory.sensors.find(value => value.id === byId('calibrationId').value);
  sensor.calibration = {enabled: true, scale: Number(byId('editCalibrationScale').value), offset: Number(byId('editCalibrationOffset').value), reference: byId('editCalibrationReference').value.trim(), calibratedBy: byId('calibrationOperator').value.trim(), calibratedAt: new Date().toISOString()};
  byId('calibrationDialog').close(); renderInventory(); markChanged(); notify('Calibration recorded. Validate the configuration before applying it.');
});

byId('commissionForm').addEventListener('submit', event => {
  if (event.submitter?.value === 'cancel') return;
  event.preventDefault();
  const equipment = editable.configuration.inventory.equipment.find(value => value.id === byId('commissionId').value);
  if (!byId('safeLoad').checked || !byId('failSafeVerified').checked || !byId('powerReturnVerified').checked || (equipment.hazardous && !byId('independentSafeguard').checked)) { notify('Every required physical safety check must be completed.'); return; }
  equipment.commissioning = {stage: 'bench-tested', safeTestLoad: true, failSafeStateVerified: true, powerReturnVerified: true, independentSafeguardPresent: byId('independentSafeguard').checked, verifiedBy: byId('commissionOperator').value.trim(), verifiedAt: new Date().toISOString()};
  byId('commissionDialog').close(); renderInventory(); markChanged(); notify('Bench evidence recorded. Apply it, then explicitly commission the equipment.');
});

byId('saveBackup').addEventListener('click', async () => {
  try {
    editable.configuration.backups = {enabled: byId('backupEnabled').checked, destination: byId('backupDestination').value.trim(), retentionDays: Number(byId('backupRetention').value)};
    markChanged();
    await validateCandidate();
    notify('Backup destination settings are valid.');
  } catch (error) { notify(error.message); }
});

async function validateCandidate() {
  if (!editable) throw new Error('Connect to AquaOS first.');
  if (byId('activateHardware').checked) {
    const config = editable.configuration;
    const equipmentByEntity = new Map(config.inventory.equipment.map(item => [item.entityId, item]));
    const uncommissioned = config.adapters.shelly.endpoints.filter(endpoint => equipmentByEntity.get(endpoint.equipmentId)?.commissioning?.stage !== 'commissioned');
    if (uncommissioned.length) throw new Error('Every mapped Shelly outlet must be commissioned before hardware activation.');
    config.simulator.enabled = false;
    config.bench.enabled = true;
    config.bench.safeLoadAcknowledged = true;
    config.adapters.shelly.enabled = config.adapters.shelly.endpoints.length > 0;
    config.adapters.esp32.enabled = config.adapters.esp32.endpoints.length > 0;
  }
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
