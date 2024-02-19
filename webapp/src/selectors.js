import manifest from './manifest';

const getPluginState = (state) => state['plugins-' + manifest.id] || {};

export const getMeetingSettingsModalState = (state) => getPluginState(state).meetingSettingsModal;
export const getMeetingSettings = (state) => getPluginState(state).meetingSettings;
