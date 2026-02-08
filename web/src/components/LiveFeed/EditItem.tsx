import type { Edit } from '../../types';
import { formatTimestamp, formatByteChange, getByteChangeColor, truncateTitle } from '../../utils/formatting';
import { Bot, User } from 'lucide-react';

interface EditItemProps {
  edit: Edit;
}

export function EditItem({ edit }: EditItemProps) {
  return (
    <div className="flex items-start space-x-3 p-2 rounded-lg hover:bg-gray-50 transition-colors text-sm">
      {/* Bot / User indicator */}
      <div className="mt-0.5">
        {edit.bot ? (
          <Bot className="h-4 w-4 text-gray-400" />
        ) : (
          <User className="h-4 w-4 text-gray-600" />
        )}
      </div>

      {/* Content */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center space-x-2">
          <span className="font-medium text-gray-900 truncate">
            {truncateTitle(edit.title, 50)}
          </span>
          {edit.bot && <span className="badge badge-bot">bot</span>}
        </div>
        <div className="flex items-center space-x-2 mt-0.5 text-xs text-gray-500">
          <span>{edit.user}</span>
          <span>·</span>
          <span>{edit.wiki}</span>
          <span>·</span>
          <span>{formatTimestamp(edit.timestamp)}</span>
        </div>
      </div>

      {/* Byte change */}
      <span className={`text-sm font-mono font-medium ${getByteChangeColor(edit.byte_change)}`}>
        {formatByteChange(edit.byte_change)}
      </span>
    </div>
  );
}
