import manifest from '../manifest';

const {id: pluginId} = manifest;
export default {
    OPEN_MEETING_SETTINGS_MODAL: pluginId + '_open_meeting_settings_modal',
    CLOSE_MEETING_SETTINGS_MODAL: pluginId + '_close_meeting_settings_modal',
    RECEIVED_MEETING_SETTINGS: pluginId + '_received_meeting_settings',
};
