
import {updateSearchTerms, updateSearchResultsTerms, updateRhsState, performSearch, openMeetingSettingsModal} from './actions';

import reducer from './reducer';

import ChannelSettingsModal from './components/meeting_settings';

import manifest from './manifest';

const {id: pluginId} = manifest;

export default class Plugin {
    initialize(registry, store) {
        registry.registerReducer(reducer);
        registry.registerWebSocketEventHandler(
            'custom_' + pluginId + '_list',
            handleSearchHashtag(store),
        );

        registry.registerRootComponent(ChannelSettingsModal);
        registry.registerChannelHeaderMenuAction('Agenda Settings',
            (channelId) => {
                store.dispatch(openMeetingSettingsModal(channelId));
            });
    }
}

function handleSearchHashtag(store) {
    return (msg) => {
        if (!msg.data) {
            return;
        }
        store.dispatch(updateSearchTerms(msg.data.hashtag));
        store.dispatch(updateSearchResultsTerms(msg.data.hashtag));

        store.dispatch(updateRhsState('search'));
        store.dispatch(performSearch(msg.data.hashtag));
    };
}

window.registerPlugin(pluginId, new Plugin());
