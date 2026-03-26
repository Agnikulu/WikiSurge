import { memo } from 'react';

interface MapSkeletonProps {
  height?: number;
}

export const MapSkeleton = memo(function MapSkeleton({ height = 400 }: MapSkeletonProps) {
  return (
    <div
      className="card relative overflow-hidden"
      style={{ height, padding: 0 }}
    >
      {/* Shimmer overlay */}
      <div
        className="absolute inset-0 animate-shimmer"
        style={{
          background:
            'linear-gradient(90deg, transparent 0%, rgba(0,255,136,0.03) 50%, transparent 100%)',
          backgroundSize: '200% 100%',
        }}
      />
      {/* Dark base matching map background */}
      <div
        className="w-full h-full flex items-center justify-center"
        style={{ background: '#111b2e' }}
      >
        <div className="text-center space-y-2">
          <div
            className="inline-block w-3 h-3 rounded-full animate-pulse"
            style={{ backgroundColor: 'rgba(0,255,136,0.15)' }}
          />
          <div
            className="text-[10px] font-mono tracking-widest"
            style={{ color: 'rgba(0,255,136,0.2)' }}
          >
            LOADING MAP DATA
          </div>
        </div>
      </div>
    </div>
  );
});
