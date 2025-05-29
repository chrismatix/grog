import React from 'react';

export interface ButtonProps {
  variant?: 'primary' | 'secondary';
  children: React.ReactNode;
  onClick?: () => void;
  className?: string;
}

export const Button: React.FC<ButtonProps> = ({
  variant = 'primary',
  children,
  onClick,
  className = '',
}) => {
  const baseStyles = 'rounded-full font-medium text-sm sm:text-base h-10 sm:h-12 px-4 sm:px-5 flex items-center justify-center transition-colors';

  const variantStyles = {
    primary: 'bg-foreground text-background hover:bg-[#383838] dark:hover:bg-[#ccc] border border-solid border-transparent',
    secondary: 'hover:bg-[#f2f2f2] dark:hover:bg-[#1a1a1a] border border-solid border-black/[.08] dark:border-white/[.145] hover:border-transparent',
  };

  return (
    <button
      className={`${baseStyles} ${variantStyles[variant]} ${className}`}
      onClick={onClick}
    >
      {children}
    </button>
  );
};
