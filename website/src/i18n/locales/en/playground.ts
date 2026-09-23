export default {
  description:
    'Make native Chat Completions or Responses calls using a personal or Project API key.',
  protocol: 'Protocol',
  key: 'API key',
  keyPlaceholder: 'Paste an enabled personal or Project key',
  invalidResponse:
    'The gateway returned an invalid native Responses payload. Received content has been preserved.',
  incomplete: 'Incomplete',
  accepted: 'Accepted, not completed',
  acceptedHelp:
    'The upstream has not completed this response. This page does not retrieve or poll stored responses.',
  incompleteHelp:
    'The response ended without completing. This turn is excluded from subsequent context.',
  failedHelp:
    'The upstream reported a failed response. This turn is excluded from subsequent context.',
  textOnly:
    'This conversation shows text and refusals. Other native output items are not rendered or replayed.',
  context:
    'Completed text replies are sent as inline history. Switching model or protocol clears the conversation.',
}
