import React from 'react';

export interface LinkProps {
  href: string;
  children: React.ReactNode;
  className?: string;
  target?: string;
  rel?: string;
  icon?: React.ReactNode;
}

export const Link: React.FC<LinkProps> = ({
  href,
  children,
  className = '',
  target = '_blank',
  rel = 'noopener noreferrer',
  icon,
}) => {
  return (
    <a
      className={`flex items-center gap-2 hover:underline hover:underline-offset-4 ${className}`}
      href={href}
      target={target}
      rel={rel}
    >
      {icon && icon}
      {children}
    </a>
  );
};
