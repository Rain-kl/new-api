/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React from 'react';
import { Modal, Spin, Typography } from '@douyinfe/semi-ui';

export default function ConversationModal({ visible, loading, content, onClose, t }) {
  let formattedContent = content;
  if (content) {
    try {
      formattedContent = JSON.stringify(JSON.parse(content), null, 2);
    } catch (e) {
      // not valid JSON, display as-is
    }
  }

  return (
    <Modal
      title={t('对话内容')}
      visible={visible}
      onCancel={onClose}
      onOk={onClose}
      okText={t('关闭')}
      cancelText={null}
      width={800}
      bodyStyle={{ maxHeight: '60vh', overflow: 'auto' }}
    >
      <Spin spinning={loading}>
        {content ? (
          <pre
            style={{
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
              fontSize: 13,
              lineHeight: 1.6,
              margin: 0,
              background: 'var(--semi-color-fill-0)',
              padding: 12,
              borderRadius: 4,
              maxHeight: '50vh',
              overflow: 'auto',
            }}
          >
            {formattedContent}
          </pre>
        ) : (
          <Typography.Text type='tertiary'>
            {t('无对话内容记录')}
          </Typography.Text>
        )}
      </Spin>
    </Modal>
  );
}
