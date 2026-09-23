export default {
  description: '使用个人或项目 API Key 发起原生 Chat Completions 或 Responses 调用。',
  protocol: '协议类型',
  key: 'API Key',
  keyPlaceholder: '粘贴已启用的个人或项目 Key',
  invalidResponse: '网关返回了无效的原生 Responses 数据，已接收内容已保留。',
  incomplete: '未完成',
  accepted: '已接受，尚未完成',
  acceptedHelp: '上游尚未完成此响应。此页面不检索或轮询已存储的响应。',
  incompleteHelp: '响应在完成前结束，本轮不会作为后续上下文。',
  failedHelp: '上游报告响应失败，本轮不会作为后续上下文。',
  textOnly: '此对话显示文本与拒绝内容，不展示或重放其他原生输出项。',
  context: '已完成的文本回复将作为内联历史发送。切换模型或协议会清空对话。',
}
