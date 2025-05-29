import React from 'react';
import Image from 'next/image';
import { Button, Link } from '@monorepo/ui-components';
import { isValidUrl } from '@monorepo/utils';

export const Header: React.FC = () => {
  const deployUrl = "https://vercel.com/new?utm_source=create-next-app&utm_medium=appdir-template-tw&utm_campaign=create-next-app";
  const docsUrl = "https://nextjs.org/docs?utm_source=create-next-app&utm_medium=appdir-template-tw&utm_campaign=create-next-app";

  // Example of using the utility function
  console.log(`Deploy URL is valid: ${isValidUrl(deployUrl)}`);

  return (
    <header className="flex flex-col gap-[32px] items-center sm:items-start">
      <Image
        className="dark:invert"
        src="/next.svg"
        alt="Next.js logo"
        width={180}
        height={38}
        priority
      />

      <div className="flex gap-4 items-center flex-col sm:flex-row">
        <Link
          href={deployUrl}
        >
          <Image
            className="dark:invert mr-2"
            src="/vercel.svg"
            alt="Vercel logomark"
            width={20}
            height={20}
          />
          Deploy now
        </Link>

        <Button variant="secondary">
          <Link href={docsUrl}>
            Read our docs
          </Link>
        </Button>
      </div>
    </header>
  );
};

export default Header;
