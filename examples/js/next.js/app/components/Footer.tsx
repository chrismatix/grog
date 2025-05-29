import React from 'react';
import Image from 'next/image';
import { Link } from '@monorepo/ui-components';
import { formatDate } from '@monorepo/utils';

export const Footer: React.FC = () => {
  // Example of using the utility function
  const currentDate = formatDate(new Date(), 'en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric'
  });

  return (
    <footer className="flex gap-[24px] flex-wrap items-center justify-center">
      <Link
        href="https://nextjs.org/learn?utm_source=create-next-app&utm_medium=appdir-template-tw&utm_campaign=create-next-app"
        icon={
          <Image
            aria-hidden
            src="/file.svg"
            alt="File icon"
            width={16}
            height={16}
          />
        }
      >
        Learn
      </Link>

      <Link
        href="https://vercel.com/templates?framework=next.js&utm_source=create-next-app&utm_medium=appdir-template-tw&utm_campaign=create-next-app"
        icon={
          <Image
            aria-hidden
            src="/window.svg"
            alt="Window icon"
            width={16}
            height={16}
          />
        }
      >
        Examples
      </Link>

      <Link
        href="https://nextjs.org?utm_source=create-next-app&utm_medium=appdir-template-tw&utm_campaign=create-next-app"
        icon={
          <Image
            aria-hidden
            src="/globe.svg"
            alt="Globe icon"
            width={16}
            height={16}
          />
        }
      >
        Go to nextjs.org →
      </Link>

      <div className="w-full text-center text-sm text-gray-500 mt-4">
        Last updated: {currentDate}
      </div>
    </footer>
  );
};

export default Footer;
