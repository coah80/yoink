let config = { WEBHOOK_URL: null, PING_USER_ID: null, ENABLED: false };
try {
    config = require('./discord-config.js');
} catch (e) {}

const lastAlertTime = new Map();
const RATE_LIMIT_MS = 60000;

const ALERT_TYPES = {
    COOKIE_ISSUE: { emoji: '🍪', color: 0xFFA500, name: 'Cookie Issue' },
    DOWNLOAD_FAILED: { emoji: '💥', color: 0xFF0000, name: 'Download Failed' },
    CONVERSION_ERROR: { emoji: '🔄', color: 0xFF0000, name: 'Conversion Error' },
    COMPRESSION_ERROR: { emoji: '📦', color: 0xFF0000, name: 'Compression Error' },
    FILE_SEND_FAILED: { emoji: '📤', color: 0xFFFF00, name: 'File Send Failed' },
    SERVER_ERROR: { emoji: '🔥', color: 0x8B0000, name: 'Server Error' },
    RATE_LIMIT: { emoji: '⚠️', color: 0x0000FF, name: 'Rate Limit Hit' },
    GALLERY_ERROR: { emoji: '🖼️', color: 0xFF0000, name: 'Gallery Download Failed' },
    METADATA_ERROR: { emoji: '📋', color: 0xFFA500, name: 'Metadata Fetch Failed' }
};

async function sendAlert(type, title, description, context = {}) {
    if (!config.ENABLED || !config.WEBHOOK_URL) {
        return false;
    }

    const alertType = ALERT_TYPES[type] || ALERT_TYPES.SERVER_ERROR;
    const alertKey = `${type}:${context.jobId || 'global'}`;

    const lastTime = lastAlertTime.get(alertKey);
    if (lastTime && Date.now() - lastTime < RATE_LIMIT_MS) {
        return false;
    }
    lastAlertTime.set(alertKey, Date.now());

    const fields = [];

    if (context.jobId) {
        fields.push({ name: 'Job ID', value: `\`${context.jobId}\``, inline: true });
    }

    if (context.error) {
        const errorMsg = context.error.length > 500 ? context.error.slice(0, 500) + '...' : context.error;
        fields.push({ name: 'Error', value: `\`\`\`${errorMsg}\`\`\``, inline: false });
    }

    if (context.format) {
        fields.push({ name: 'Format', value: context.format, inline: true });
    }

    const payload = {
        content: config.PING_USER_ID ? `<@${config.PING_USER_ID}>` : null,
        embeds: [{
            title: `${alertType.emoji} ${alertType.name}: ${title}`,
            description: description,
            color: alertType.color,
            fields: fields,
            timestamp: new Date().toISOString(),
            footer: { text: 'Yoink Server Monitor' }
        }]
    };

    try {
        const response = await fetch(config.WEBHOOK_URL, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        if (!response.ok) {
            return false;
        }

        return true;
    } catch (err) {
        return false;
    }
}

const alerts = {
    cookieIssue: (title, description, context) =>
        sendAlert('COOKIE_ISSUE', title, description, context),

    downloadFailed: (title, description, context) =>
        sendAlert('DOWNLOAD_FAILED', title, description, context),

    conversionError: (title, description, context) =>
        sendAlert('CONVERSION_ERROR', title, description, context),

    compressionError: (title, description, context) =>
        sendAlert('COMPRESSION_ERROR', title, description, context),

    fileSendFailed: (title, description, context) =>
        sendAlert('FILE_SEND_FAILED', title, description, context),

    serverError: (title, description, context) =>
        sendAlert('SERVER_ERROR', title, description, context),

    rateLimit: (title, description, context) =>
        sendAlert('RATE_LIMIT', title, description, context),

    galleryError: (title, description, context) =>
        sendAlert('GALLERY_ERROR', title, description, context),

    metadataError: (title, description, context) =>
        sendAlert('METADATA_ERROR', title, description, context),

    test: () => sendAlert('SERVER_ERROR', 'Test Alert', 'This is a test notification from Yoink server.', {
        jobId: 'test-' + Date.now(),
        error: 'No actual error - this is just a test!'
    }),

    isEnabled: () => config.ENABLED && !!config.WEBHOOK_URL
};

module.exports = alerts;
